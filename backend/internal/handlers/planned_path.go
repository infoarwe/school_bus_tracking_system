package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/tracking"
)

// PathProvider computes the road path through a list of points
// (tracking.GoogleRoutes). Optional: without it the path is straight lines.
type PathProvider interface {
	Path(ctx context.Context, apiKey string, pts []tracking.LatLng) ([]tracking.LatLng, error)
}

const (
	PathSourceGoogle   = "google"
	PathSourceStraight = "straight"
)

type plannedPathResponse struct {
	// google: follows the roads; straight: stop to stop (no server key, or Google failed).
	Source string            `json:"source"`
	Points []tracking.LatLng `json:"points"`
}

// TripPlannedPath (admin): the road path the bus is meant to drive, through the
// trip's stops in travel order (School first for Evening Drop and Special Trip,
// School last for Morning Pickup and Special Trip). Compared on the live map
// with the GPS track to spot deviations.
func (a *API) TripPlannedPath(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "tripID")
	if !ok {
		return
	}
	ctx := r.Context()
	schoolID := chi.URLParam(r, "schoolID")
	t, err := store.GetTrip(ctx, a.Store.Pool, schoolID, id)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	pts, _, err := a.tripWaypoints(ctx, t)
	if err != nil {
		internalError(w, r, err)
		return
	}
	res := plannedPathResponse{Source: PathSourceStraight, Points: pts}
	if road := a.roadPath(ctx, schoolID, pts); road != nil {
		res = plannedPathResponse{Source: PathSourceGoogle, Points: road}
	}
	httpx.JSON(w, http.StatusOK, res)
}

// tripWaypoints lists the points the bus visits, in order, and the stops among
// them (travel order, the school as tracking.SchoolStopID).
func (a *API) tripWaypoints(ctx context.Context, t *models.Trip) ([]tracking.LatLng, []tracking.StopInput, error) {
	stops, err := a.tripStops(ctx, t)
	if err != nil {
		return nil, nil, err
	}
	settings, err := store.GetTrackingSettings(ctx, a.Store.Pool, t.SchoolID)
	if err != nil {
		return nil, nil, err
	}
	plain := make([]models.Stop, len(stops))
	for i, s := range stops {
		plain[i] = s.Stop
	}
	pts := make([]tracking.LatLng, 0, len(stops)+2)
	// Evening Drop and Special Trip leave from the school (not a tracked stop).
	if t.TripType != models.TripMorningPickup && settings.SchoolLatitude != nil && settings.SchoolLongitude != nil {
		pts = append(pts, tracking.LatLng{Latitude: *settings.SchoolLatitude, Longitude: *settings.SchoolLongitude})
	}
	ordered := tripStopsInOrder(t, plain, settings)
	for _, s := range ordered {
		pts = append(pts, tracking.LatLng{Latitude: s.Latitude, Longitude: s.Longitude})
	}
	return pts, ordered, nil
}

// roadPath asks Google (school's server key) for the path, cached in Redis by
// the exact points. nil means "draw straight lines".
func (a *API) roadPath(ctx context.Context, schoolID string, pts []tracking.LatLng) []tracking.LatLng {
	g, ok := a.ETA.(PathProvider)
	if !ok || len(pts) < 2 {
		return nil
	}
	raw, _ := json.Marshal(pts)
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	if cached, err := a.Live.PlannedPath(ctx, hash); err == nil && cached != nil {
		return cached
	}
	m, err := store.GetMapsSettings(ctx, a.Store.Pool, schoolID)
	if err != nil || m.ServerKeyEncrypted == nil {
		return nil
	}
	key, err := a.Secrets.Decrypt(m.ServerKeyEncrypted)
	if err != nil {
		slog.Error("decrypt maps server key", "school_id", schoolID, "err", err)
		return nil
	}
	gctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	road, err := g.Path(gctx, key, pts)
	if err != nil {
		slog.Warn("google route path failed; drawing straight lines", "school_id", schoolID, "err", err)
		return nil
	}
	if err := a.Live.SavePlannedPath(ctx, hash, road); err != nil {
		slog.Error("save planned path", "err", err)
	}
	return road
}
