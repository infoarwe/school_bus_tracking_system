package handlers

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

const (
	importMaxBytes = 2 << 20
	importMaxRows  = 2000
)

// ImportColumns is the CSV header the import accepts (order does not matter).
// admission_no and student_name are required; the rest are optional.
var ImportColumns = []string{
	"admission_no", "student_name", "class", "section",
	"parent_name", "parent_mobile", "parent_relationship",
	"parent2_name", "parent2_mobile", "parent2_relationship",
	"route_code", "pickup_stop", "drop_stop",
}

type importRowError struct {
	Row     int    `json:"row"` // line number in the file (header is line 1)
	Field   string `json:"field"`
	Message string `json:"message"`
}

type importResult struct {
	DryRun          bool             `json:"dry_run"`
	Imported        bool             `json:"imported"`
	TotalRows       int              `json:"total_rows"`
	StudentsCreated int              `json:"students_created"`
	StudentsUpdated int              `json:"students_updated"`
	ParentsCreated  int              `json:"parents_created"`
	ParentLinks     int              `json:"parent_links"`
	AssignmentsSet  int              `json:"assignments_set"`
	Errors          []importRowError `json:"errors"`
}

// rowError aborts one row with field errors.
type rowError struct{ fields validate.Errors }

func (e *rowError) Error() string { return "row invalid" }

func rowFail(field, msg string) error { return &rowError{validate.Errors{field: msg}} }

// ImportStudents reads a CSV of students, parents and (optionally) route/stops.
// All or nothing: if any row has an error nothing is saved and every error is
// returned. ?dry_run=true validates without saving. Existing students are matched
// by admission number and updated; parents are matched by mobile.
func (a *API) ImportStudents(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	dryRun, _ := strconv.ParseBool(r.URL.Query().Get("dry_run"))

	r.Body = http.MaxBytesReader(w, r.Body, importMaxBytes)
	file, _, err := r.FormFile("file")
	if err != nil {
		httpx.ValidationError(w, validate.Errors{"file": "upload a CSV file (max 2 MB) in the 'file' field"})
		return
	}
	defer file.Close()

	records, header, perr := readImportCSV(file)
	if perr != nil {
		httpx.ValidationError(w, validate.Errors{"file": perr.Error()})
		return
	}

	ctx := r.Context()
	res := &importResult{DryRun: dryRun, TotalRows: len(records), Errors: []importRowError{}}
	tx, err := a.Store.Pool.Begin(ctx)
	if err != nil {
		internalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	imp := &importer{a: a, r: r, schoolID: schoolID, res: res, routes: map[string]*importRoute{}}
	for i, rec := range records {
		line := i + 2
		row := map[string]string{}
		for j, col := range header {
			if j < len(rec) {
				row[col] = strings.TrimSpace(rec[j])
			}
		}
		// Each row runs in a savepoint so one bad row does not hide errors in later rows.
		sp, err := tx.Begin(ctx)
		if err != nil {
			internalError(w, r, err)
			return
		}
		counts := *res // the counters are undone with the row's savepoint
		err = imp.row(ctx, sp, row)
		var re *rowError
		switch {
		case errors.As(err, &re):
			_ = sp.Rollback(ctx)
			counts.Errors = res.Errors
			*res = counts
			for field, msg := range re.fields {
				res.Errors = append(res.Errors, importRowError{Row: line, Field: field, Message: msg})
			}
		case err != nil:
			_ = sp.Rollback(ctx)
			internalError(w, r, fmt.Errorf("import line %d: %w", line, err))
			return
		default:
			if err := sp.Commit(ctx); err != nil {
				internalError(w, r, err)
				return
			}
		}
	}

	if len(res.Errors) == 0 && !dryRun {
		summary := *res
		summary.Errors = nil
		if err := a.audit(ctx, tx, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "student.import", EntityType: "student", After: summary,
		}); err != nil {
			internalError(w, r, err)
			return
		}
		if err := tx.Commit(ctx); err != nil {
			internalError(w, r, err)
			return
		}
		res.Imported = true
	}
	httpx.JSON(w, http.StatusOK, res)
}

func readImportCSV(f io.Reader) ([][]string, []string, error) {
	cr := csv.NewReader(f)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	all, err := cr.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("not a valid CSV file: %v", err)
	}
	if len(all) < 2 {
		return nil, nil, errors.New("the file has no data rows")
	}
	if len(all)-1 > importMaxRows {
		return nil, nil, fmt.Errorf("at most %d rows per file", importMaxRows)
	}
	known := map[string]bool{}
	for _, c := range ImportColumns {
		known[c] = true
	}
	header := make([]string, len(all[0]))
	seen := map[string]bool{}
	for i, h := range all[0] {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, string(rune(0xFEFF)))))
		h = strings.ReplaceAll(h, " ", "_")
		if !known[h] {
			return nil, nil, fmt.Errorf("unknown column %q; expected: %s", h, strings.Join(ImportColumns, ", "))
		}
		header[i] = h
		seen[h] = true
	}
	if !seen["admission_no"] || !seen["student_name"] {
		return nil, nil, errors.New("columns admission_no and student_name are required")
	}
	// Skip blank lines.
	var records [][]string
	for _, rec := range all[1:] {
		if strings.TrimSpace(strings.Join(rec, "")) != "" {
			records = append(records, rec)
		}
	}
	return records, header, nil
}

type importRoute struct {
	route *models.Route
	stops []models.Stop
}

type importer struct {
	a        *API
	r        *http.Request
	schoolID string
	res      *importResult
	routes   map[string]*importRoute // by upper-case code; nil value = not found
}

func (imp *importer) row(ctx context.Context, q pgx.Tx, row map[string]string) error {
	in, errs := (&studentRequest{
		AdmissionNo: row["admission_no"], Name: row["student_name"], Class: row["class"], Section: row["section"],
	}).toInput()
	if len(errs) > 0 {
		renamed := validate.Errors{}
		for f, m := range errs {
			if f == "name" {
				f = "student_name"
			}
			renamed[f] = m
		}
		return &rowError{renamed}
	}

	// Student: update if the admission number exists, else create.
	student, err := store.GetStudentByAdmissionNo(ctx, q, imp.schoolID, in.AdmissionNo)
	switch {
	case errors.Is(err, store.ErrNotFound):
		id, err := store.CreateStudent(ctx, q, imp.schoolID, in)
		if err != nil {
			return err
		}
		if student, err = store.GetStudent(ctx, q, imp.schoolID, id); err != nil {
			return err
		}
		imp.res.StudentsCreated++
	case err != nil:
		return err
	default:
		if student.Name != in.Name || student.Class != in.Class || student.Section != in.Section {
			in.Notes, in.TransportStatus = *student.Notes, student.TransportStatus
			if err := store.UpdateStudent(ctx, q, imp.schoolID, student.ID, in); err != nil {
				return err
			}
			imp.res.StudentsUpdated++
		}
	}

	for _, prefix := range []string{"parent", "parent2"} {
		if err := imp.linkParent(ctx, q, student.ID, row[prefix+"_name"], row[prefix+"_mobile"], row[prefix+"_relationship"], prefix); err != nil {
			return err
		}
	}

	if code := strings.ToUpper(row["route_code"]); code != "" {
		return imp.assign(ctx, q, student, code, row["pickup_stop"], row["drop_stop"])
	}
	if row["pickup_stop"] != "" || row["drop_stop"] != "" {
		return rowFail("route_code", "is required when a stop is given")
	}
	return nil
}

func (imp *importer) linkParent(ctx context.Context, q pgx.Tx, studentID, name, mobile, relationship, prefix string) error {
	if mobile == "" {
		if name != "" {
			return rowFail(prefix+"_mobile", "is required when "+prefix+"_name is given")
		}
		return nil
	}
	m, ok := validate.NormalizeMobile(mobile)
	if !ok {
		return rowFail(prefix+"_mobile", "must be a valid 10-digit mobile number")
	}
	relationship = strings.ToLower(relationship)
	if relationship == "" {
		relationship = "guardian"
	}
	if !contains(relationships, relationship) {
		return rowFail(prefix+"_relationship", "must be father, mother or guardian")
	}

	var parentID string
	u, err := store.GetUserByMobile(ctx, q, m, models.RoleParent)
	switch {
	case errors.Is(err, store.ErrNotFound):
		if strings.TrimSpace(name) == "" {
			return rowFail(prefix+"_name", "is required for a new parent")
		}
		u, err = store.CreateUser(ctx, q, store.UserInput{SchoolID: &imp.schoolID, Role: models.RoleParent, Name: strings.TrimSpace(name), Mobile: &m})
		if err != nil {
			return err
		}
		if parentID, err = store.CreateParent(ctx, q, imp.schoolID, u.ID, store.ParentProfile{}); err != nil {
			return err
		}
		imp.res.ParentsCreated++
	case err != nil:
		return err
	default:
		if u.SchoolID == nil || *u.SchoolID != imp.schoolID {
			return rowFail(prefix+"_mobile", "this number is registered as a parent at another school")
		}
		p, err := store.GetParentByUser(ctx, q, u.ID)
		if err != nil {
			return err
		}
		parentID = p.ID
	}
	if err := store.LinkParentStudent(ctx, q, imp.schoolID, parentID, store.ChildInput{StudentID: studentID, Relationship: relationship}); err != nil {
		return err
	}
	imp.res.ParentLinks++
	return nil
}

func (imp *importer) assign(ctx context.Context, q pgx.Tx, student *models.Student, code, pickup, drop string) error {
	ir, cached := imp.routes[code]
	if !cached {
		var err error
		ir, err = imp.loadRoute(ctx, q, code)
		if err != nil {
			return err
		}
		imp.routes[code] = ir
	}
	if ir == nil {
		return rowFail("route_code", "no route with code "+code)
	}
	// One stop given and the route runs both trips: use it for both.
	if ir.route.SupportsPickup && ir.route.SupportsDrop {
		if pickup == "" {
			pickup = drop
		}
		if drop == "" {
			drop = pickup
		}
	}
	req := assignmentRequest{RouteID: ir.route.ID}
	var err error
	if req.PickupStopID, err = findStop(ir.stops, pickup, "pickup_stop"); err != nil {
		return err
	}
	if req.DropStopID, err = findStop(ir.stops, drop, "drop_stop"); err != nil {
		return err
	}
	if _, err := validateAssignment(imp.r, q, imp.schoolID, student, &req); err != nil {
		var ae *errAssignment
		if errors.As(err, &ae) {
			return &rowError{ae.fields}
		}
		return err
	}
	if cur := student.Assignment; cur != nil && cur.RouteID == req.RouteID &&
		sameStop(cur.PickupStop, req.PickupStopID) && sameStop(cur.DropStop, req.DropStopID) {
		return nil
	}
	if err := store.AssignStudent(ctx, q, imp.schoolID, student.ID, req.RouteID, req.PickupStopID, req.DropStopID,
		auth.FromContext(ctx).UserID); err != nil {
		return err
	}
	imp.res.AssignmentsSet++
	return nil
}

func (imp *importer) loadRoute(ctx context.Context, q pgx.Tx, code string) (*importRoute, error) {
	var id string
	err := q.QueryRow(ctx, `SELECT id FROM routes WHERE school_id = $1 AND code = $2`, imp.schoolID, code).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	route, err := store.GetRoute(ctx, q, imp.schoolID, id)
	if err != nil {
		return nil, err
	}
	stops, err := store.ListStops(ctx, q, id)
	if err != nil {
		return nil, err
	}
	return &importRoute{route: route, stops: stops}, nil
}

// findStop matches a stop by sequence number ("2") or name (case-insensitive).
func findStop(stops []models.Stop, ref, field string) (*string, error) {
	if ref == "" {
		return nil, nil
	}
	if n, err := strconv.Atoi(ref); err == nil {
		for _, s := range stops {
			if s.Sequence == n {
				return &s.ID, nil
			}
		}
		return nil, rowFail(field, fmt.Sprintf("the route has no stop number %d", n))
	}
	var found *string
	for _, s := range stops {
		if strings.EqualFold(s.Name, ref) {
			if found != nil {
				return nil, rowFail(field, "more than one stop is named "+ref+"; use the stop number")
			}
			id := s.ID
			found = &id
		}
	}
	if found == nil {
		return nil, rowFail(field, "the route has no stop named "+ref)
	}
	return found, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
