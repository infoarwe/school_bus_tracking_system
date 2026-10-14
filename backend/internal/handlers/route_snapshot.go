package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/storage"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/tracking"
)

// StaticMapper renders a map image (tracking.StaticMap). nil disables route images.
type StaticMapper interface {
	Render(ctx context.Context, apiKey string, paths []tracking.MapPath, markers []tracking.MapMarker) ([]byte, error)
}

// Encoded-path budgets inside the 16 384-character Static Maps URL.
const (
	travelledPathMaxLen = 7000
	plannedPathMaxLen   = 4000
)

// routeImageKey is where a trip's route image is stored (storage.Store key).
func routeImageKey(schoolID, tripID string) string {
	return fmt.Sprintf("school_%s/trips/%s.png", schoolID, tripID)
}

// snapshotRouteLater makes the trip's route snapshot in the background after it
// was completed (the driver's "End trip" or a staff override must not wait for Google).
func (a *API) snapshotRouteLater(t *models.Trip) {
	if a.StaticMaps == nil || a.Files == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		// One snapshot per trip at a time, across API instances.
		lock := "route_snapshot:" + t.ID
		if ok, err := a.Live.TryLock(ctx, lock, 2*time.Minute); err != nil || !ok {
			return
		}
		defer a.Live.Unlock(context.Background(), lock)
		if _, err := a.makeRouteSnapshot(ctx, t); err != nil {
			slog.Error("route snapshot", "trip_id", t.ID, "err", err)
		}
	}()
}

// makeRouteSnapshot draws the travelled path (from the stored GPS track) over the
// planned route and stops, stores the PNG and saves the snapshot row. Problems
// with Google are saved on the row (status failed) rather than returned.
func (a *API) makeRouteSnapshot(ctx context.Context, t *models.Trip) (*store.RouteSnapshot, error) {
	track, err := store.TripTrack(ctx, a.Store.Pool, t.ID)
	if err != nil {
		return nil, err
	}
	snap := store.RouteSnapshot{TripID: t.ID, StartedAt: t.StartedAt, EndedAt: t.EndedAt, PointCount: len(track)}
	travelled := make([]tracking.LatLng, len(track))
	for i, p := range track {
		travelled[i] = tracking.LatLng{Latitude: p.Latitude, Longitude: p.Longitude}
		if i > 0 {
			snap.DistanceM += int(tracking.DistanceM(track[i-1].Latitude, track[i-1].Longitude, p.Latitude, p.Longitude))
		}
	}
	save := func(status, msg string) (*store.RouteSnapshot, error) {
		snap.Status, snap.Error = status, msg
		if err := store.SaveRouteSnapshot(ctx, a.Store.Pool, t.SchoolID, snap); err != nil {
			return nil, err
		}
		snap.HasImage = snap.ImageKey != nil
		return &snap, nil
	}
	if len(travelled) < 2 {
		return save(store.SnapshotNoGPS, "No GPS history for this trip (Settings → Live tracking keeps history only when its days are above 0).")
	}
	snap.Polyline = tracking.EncodeWithin(travelled, travelledPathMaxLen)

	if a.StaticMaps == nil || a.Files == nil {
		return save(store.SnapshotFailed, "Route images are not enabled on this server.")
	}
	m, err := store.GetMapsSettings(ctx, a.Store.Pool, t.SchoolID)
	if err != nil {
		return nil, err
	}
	if m.ServerKeyEncrypted == nil {
		return save(store.SnapshotFailed, "No Google server key: add one in Settings → Maps (with Maps Static API enabled).")
	}
	key, err := a.Secrets.Decrypt(m.ServerKeyEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt maps server key: %w", err)
	}

	planned, stops, err := a.tripWaypoints(ctx, t)
	if err != nil {
		return nil, err
	}
	if road := a.roadPath(ctx, t.SchoolID, planned); road != nil {
		planned = road
	}
	settings, err := store.GetTrackingSettings(ctx, a.Store.Pool, t.SchoolID)
	if err != nil {
		return nil, err
	}
	var paths []tracking.MapPath
	if len(planned) > 1 {
		paths = append(paths, tracking.MapPath{Encoded: tracking.EncodeWithin(planned, plannedPathMaxLen), Color: "0x1d4ed8aa", Weight: 6})
	}
	paths = append(paths, tracking.MapPath{Encoded: snap.Polyline, Color: "0x16a34aff", Weight: 4})

	var markers []tracking.MapMarker
	if settings.SchoolLatitude != nil && settings.SchoolLongitude != nil {
		markers = append(markers, tracking.MapMarker{Color: "0x7c3aed", Label: "S",
			At: tracking.LatLng{Latitude: *settings.SchoolLatitude, Longitude: *settings.SchoolLongitude}})
	}
	n := 0
	for _, s := range stops {
		if s.ID == tracking.SchoolStopID {
			continue
		}
		n++
		label := ""
		if n <= 9 {
			label = strconv.Itoa(n)
		}
		markers = append(markers, tracking.MapMarker{Color: "0x1d4ed8", Label: label, Size: "mid",
			At: tracking.LatLng{Latitude: s.Latitude, Longitude: s.Longitude}})
	}
	markers = append(markers,
		tracking.MapMarker{Color: "green", Label: "A", Size: "mid", At: travelled[0]},
		tracking.MapMarker{Color: "red", Label: "B", Size: "mid", At: travelled[len(travelled)-1]})

	png, err := a.StaticMaps.Render(ctx, key, paths, markers)
	if err != nil {
		slog.Warn("route snapshot image failed", "trip_id", t.ID, "err", err)
		return save(store.SnapshotFailed, "Google could not draw the map. Check that Maps Static API is enabled for the school's server key, then try again.")
	}
	imageKey := routeImageKey(t.SchoolID, t.ID)
	if err := a.Files.Put(ctx, imageKey, png); err != nil {
		return nil, err
	}
	snap.ImageKey = &imageKey
	return save(store.SnapshotReady, "")
}

// GetRouteSnapshot (admin): the completed trip's travelled-route summary.
func (a *API) GetRouteSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	s, err := store.GetRouteSnapshot(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"), id)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, s)
}

// RouteSnapshotImage (admin): the PNG map of the travelled route.
func (a *API) RouteSnapshotImage(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	ctx := r.Context()
	s, err := store.GetRouteSnapshot(ctx, a.Store.Pool, chi.URLParam(r, "schoolID"), id)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	if s.ImageKey == nil || a.Files == nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "This trip has no route image.")
		return
	}
	f, err := a.Files.Open(ctx, *s.ImageKey)
	if errors.Is(err, storage.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "not_found", "This trip has no route image.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'")
	h.Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil {
		slog.Debug("send route image", "err", err)
	}
}

// CreateRouteSnapshot (admin): make (or remake) a completed trip's route snapshot
// now, e.g. after enabling Maps Static API or for a trip completed before this existed.
func (a *API) CreateRouteSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	ctx := r.Context()
	t, err := store.GetTrip(ctx, a.Store.Pool, chi.URLParam(r, "schoolID"), id)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	if t.Status != models.TripCompleted {
		httpx.Error(w, http.StatusConflict, "trip_not_completed", "The route image is made when the trip is completed.")
		return
	}
	lock := "route_snapshot:" + t.ID
	if ok, err := a.Live.TryLock(ctx, lock, 2*time.Minute); err != nil || !ok {
		httpx.Error(w, http.StatusConflict, "snapshot_in_progress", "The route image is being made. Try again in a minute.")
		return
	}
	defer a.Live.Unlock(context.Background(), lock)
	s, err := a.makeRouteSnapshot(ctx, t)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if err := a.audit(ctx, a.Store.Pool, r, store.AuditEntry{SchoolID: &t.SchoolID, Action: "trip.route_snapshot",
		EntityType: "trip", EntityID: &t.ID, After: map[string]any{"status": s.Status}}); err != nil {
		internalErrorLog(r, err)
	}
	httpx.JSON(w, http.StatusOK, s)
}
