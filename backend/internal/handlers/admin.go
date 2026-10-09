package handlers

// Sprint 8: dashboard, reports (JSON preview + CSV), audit log viewer, trip replay.

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

const (
	maxReportDays    = 366
	maxReportCSVRows = 200000
	maxReportPreview = 500
)

// Dashboard: totals, today's trips, live and stale buses, delays, emergencies, per-route summary.
func (a *API) Dashboard(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	d, err := store.GetDashboard(r.Context(), a.Store.Pool, schoolID)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	buses, err := a.Live.Snapshot(r.Context(), schoolID)
	if err != nil {
		liveUnavailable(w, r, err)
		return
	}
	d.Live.OnRoad = len(buses)
	for _, b := range buses {
		if b.Stale {
			d.Live.Offline++
		}
	}
	httpx.JSON(w, http.StatusOK, d)
}

// ListReports describes the available reports (for the Reports page).
func (a *API) ListReports(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, store.Reports)
}

// reportRequest validates the report key and filters from the query string.
func reportRequest(w http.ResponseWriter, r *http.Request) (*store.ReportDef, store.ReportFilter, bool) {
	rep, ok := store.FindReport(chi.URLParam(r, "report"))
	if !ok {
		httpx.Error(w, http.StatusNotFound, "not_found", "No such report.")
		return nil, store.ReportFilter{}, false
	}
	qs := r.URL.Query()
	f := store.ReportFilter{From: qs.Get("from"), To: qs.Get("to"), RouteID: qs.Get("route_id"), BusID: qs.Get("bus_id"),
		DriverID: qs.Get("driver_id"), TripID: qs.Get("trip_id")}
	v := validate.New()
	uses := map[string]bool{}
	for _, name := range rep.Filters {
		uses[name] = true
	}
	if uses["from"] {
		from, err1 := time.Parse(time.DateOnly, f.From)
		to, err2 := time.Parse(time.DateOnly, f.To)
		v.Check(err1 == nil, "from", "must be a date YYYY-MM-DD")
		v.Check(err2 == nil, "to", "must be a date YYYY-MM-DD")
		if err1 == nil && err2 == nil {
			v.Check(!to.Before(from), "to", "must not be before from")
			v.Check(to.Sub(from) <= maxReportDays*24*time.Hour, "to", "the period can be at most one year")
		}
	}
	for _, name := range []string{"route_id", "bus_id", "driver_id", "trip_id"} {
		val := f.Value(name)
		if uses[name] && val != "" {
			_, err := uuid.Parse(val)
			v.Check(err == nil, name, "must be an ID")
		}
	}
	if uses["trip_id"] {
		v.Required("trip_id", f.TripID)
	}
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return nil, f, false
	}
	return rep, f, true
}

// RunReport: JSON preview (paged, up to 500 rows a page) or the full CSV with ?format=csv.
func (a *API) RunReport(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	rep, f, ok := reportRequest(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if r.URL.Query().Get("format") == "csv" {
		a.reportCSV(w, r, rep, schoolID, f)
		return
	}
	pg := page(r)
	if pg.PageSize > maxReportPreview {
		pg.PageSize = maxReportPreview
	}
	total, err := store.CountReport(ctx, a.Store.Pool, rep, schoolID, f)
	if err != nil {
		internalError(w, r, err)
		return
	}
	rows := [][]string{}
	if err := store.RunReport(ctx, a.Store.Pool, rep, schoolID, f, pg.PageSize, pg.Offset(), func(row []string) error {
		rows = append(rows, row)
		return nil
	}); err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSONWithMeta(w, map[string]any{"report": rep, "rows": rows}, map[string]any{"page": pg.Page, "page_size": pg.PageSize, "total": total})
}

func (a *API) reportCSV(w http.ResponseWriter, r *http.Request, rep *store.ReportDef, schoolID string, f store.ReportFilter) {
	name := rep.Key
	if f.From != "" {
		name += "_" + f.From + "_" + f.To
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.csv"`)
	// Byte-order mark so Excel opens UTF-8 (names in Tamil etc.) correctly.
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(w)
	header := make([]string, len(rep.Columns))
	for i, c := range rep.Columns {
		header[i] = c.Label
	}
	_ = cw.Write(header)
	n := 0
	err := store.RunReport(r.Context(), a.Store.Pool, rep, schoolID, f, maxReportCSVRows, 0, func(row []string) error {
		for i := range row {
			row[i] = csvSafe(row[i])
		}
		n++
		return cw.Write(row)
	})
	cw.Flush()
	if err != nil {
		internalErrorLog(r, err) // headers are sent; the file ends early
	}
	_ = a.Store.InTx(r.Context(), func(q store.DBTX) error {
		return a.audit(r.Context(), q, r, store.AuditEntry{SchoolID: &schoolID, Action: "report.export", EntityType: "report",
			After: map[string]any{"report": rep.Key, "from": f.From, "to": f.To, "rows": n}})
	})
}

// csvSafe stops spreadsheet formula injection: a cell starting with = + - @ (or
// tab/CR) is shown as text. Negative numbers stay numbers.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return s
	}
	if strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// transportEntities are the audit entries a Transport Manager may see ("Audit Logs: Transport").
var transportEntities = []string{"driver", "bus", "route", "stop", "trip", "emergency"}

func auditFilter(r *http.Request) store.AuditFilter {
	qs := r.URL.Query()
	return store.AuditFilter{EntityType: qs.Get("entity_type"), EntityID: qs.Get("entity_id"), Action: qs.Get("action"),
		ActorID: qs.Get("actor_id"), From: qs.Get("from"), To: qs.Get("to")}
}

// SchoolAuditLogs: Super Admin and School Admin see the whole school; a Transport
// Manager only transport entries.
func (a *API) SchoolAuditLogs(w http.ResponseWriter, r *http.Request) {
	f := auditFilter(r)
	f.SchoolID = chi.URLParam(r, "schoolID")
	if auth.FromContext(r.Context()).Role == models.RoleTransportManager {
		f.EntityTypes = transportEntities
	}
	a.listAudit(w, r, f)
}

// PlatformAuditLogs: Super Admin, every school plus platform-level entries
// (?school_id= narrows to one school).
func (a *API) PlatformAuditLogs(w http.ResponseWriter, r *http.Request) {
	f := auditFilter(r)
	if id := r.URL.Query().Get("school_id"); id != "" {
		f.SchoolID = id
	} else {
		f.AllSchools = true
	}
	a.listAudit(w, r, f)
}

func (a *API) listAudit(w http.ResponseWriter, r *http.Request, f store.AuditFilter) {
	v := validate.New()
	for field, val := range map[string]string{"from": f.From, "to": f.To} {
		if val != "" {
			_, err := time.Parse(time.DateOnly, val)
			v.Check(err == nil, field, "must be a date YYYY-MM-DD")
		}
	}
	for field, val := range map[string]string{"entity_id": f.EntityID, "actor_id": f.ActorID, "school_id": f.SchoolID} {
		if val != "" {
			_, err := uuid.Parse(val)
			v.Check(err == nil, field, "must be an ID")
		}
	}
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}
	pg := page(r)
	rows, total, err := store.ListAudit(r.Context(), a.Store.Pool, f, pg)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.List(w, rows, meta(pg, total))
}

// TripTrack: the stored GPS track of a trip, for replay on the map.
func (a *API) TripTrack(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	if _, err := store.GetTrip(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), id); err != nil {
		storeError(w, r, err, nil)
		return
	}
	track, err := store.TripTrack(r.Context(), a.Store.Pool, id)
	if err != nil {
		internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, track)
}
