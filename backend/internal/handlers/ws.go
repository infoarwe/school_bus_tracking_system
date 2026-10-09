package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/middleware"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/tracking"
)

const (
	wsPingInterval    = 25 * time.Second
	wsSessionRecheck  = 60 * time.Second
	wsWriteTimeout    = 10 * time.Second
	wsMaxMessageBytes = 4096

	// Close codes the apps should handle (4000-4999 are application-defined).
	wsCloseUnauthorized = websocket.StatusCode(4401) // log in again (or refresh the token) and reconnect
)

// wsIn is a message from the client.
type wsIn struct {
	Type    string `json:"type"`    // subscribe | unsubscribe | ping
	Channel string `json:"channel"` // school | trip
	TripID  string `json:"trip_id"`
}

// wsOut is a reply to the client (events from the hub are sent as tracking.Event).
type wsOut struct {
	Type     string                 `json:"type"` // subscribed | unsubscribed | pong | error
	Channel  string                 `json:"channel,omitempty"`
	TripID   string                 `json:"trip_id,omitempty"`
	Status   string                 `json:"status,omitempty"`   // trip status at subscribe time
	Snapshot []tracking.BusLocation `json:"snapshot,omitempty"` // current positions
	Code     string                 `json:"code,omitempty"`
	Message  string                 `json:"message,omitempty"`
}

// LiveSocket is the WebSocket endpoint for live tracking. Browsers cannot set
// headers on a WebSocket, so the access token comes as ?access_token=. A Super
// Admin also passes ?school_id=. Every subscription is authorized for the
// caller's role and school (rule 2), and the session is re-checked every minute.
func (a *API) LiveSocket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := middleware.LoadPrincipal(ctx, a.Tokens, a.Store, r.URL.Query().Get("access_token"))
	var ae *middleware.AuthError
	if errors.As(err, &ae) {
		httpx.Error(w, ae.Status, ae.Code, ae.Message)
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	schoolID, ok := a.socketSchool(w, r, p)
	if !ok {
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: a.WSOriginPatterns})
	if err != nil {
		return // Accept has written the error response
	}
	conn.SetReadLimit(wsMaxMessageBytes)
	defer conn.CloseNow()

	client := tracking.NewClient(schoolID)
	a.Hub.Register(client)
	defer a.Hub.Unregister(client)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx = auth.WithPrincipal(ctx, p)

	// Reader: handles subscribe/unsubscribe/ping until the client goes away.
	go func() {
		defer cancel()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var in wsIn
			if json.Unmarshal(data, &in) != nil {
				a.wsReply(client, wsOut{Type: "error", Code: "bad_request", Message: "Messages must be JSON."})
				continue
			}
			a.wsHandle(ctx, p, client, in)
		}
	}()

	ping := time.NewTicker(wsPingInterval)
	defer ping.Stop()
	recheck := time.NewTicker(wsSessionRecheck)
	defer recheck.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-client.Closed:
			conn.Close(websocket.StatusPolicyViolation, "too slow")
			return
		case msg := <-client.Send:
			wctx, wcancel := context.WithTimeout(ctx, wsWriteTimeout)
			err := conn.Write(wctx, websocket.MessageText, msg)
			wcancel()
			if err != nil {
				return
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, wsWriteTimeout)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		case <-recheck.C:
			// Logout, suspension or school deactivation ends the live stream too.
			if _, err := middleware.RecheckSession(ctx, a.Store, p.UserID, p.SessionID); err != nil {
				conn.Close(wsCloseUnauthorized, "session ended")
				return
			}
		}
	}
}

// socketSchool: staff and app users are bound to their own school; a Super Admin picks one.
func (a *API) socketSchool(w http.ResponseWriter, r *http.Request, p *auth.Principal) (string, bool) {
	if !p.IsSuperAdmin() {
		return *p.SchoolID, true
	}
	id := r.URL.Query().Get("school_id")
	if _, err := uuid.Parse(id); err != nil {
		httpx.Error(w, http.StatusBadRequest, "school_required", "Super Admin must pass ?school_id=.")
		return "", false
	}
	if _, err := store.GetSchool(r.Context(), a.Store.Pool, id); err != nil {
		storeError(w, r, err, nil)
		return "", false
	}
	return id, true
}

func (a *API) wsReply(c *tracking.Client, out wsOut) {
	raw, _ := json.Marshal(out)
	select {
	case c.Send <- raw:
	default:
		c.Close()
	}
}

func (a *API) wsHandle(ctx context.Context, p *auth.Principal, c *tracking.Client, in wsIn) {
	switch in.Type {
	case "ping":
		a.wsReply(c, wsOut{Type: "pong"})
	case "subscribe", "unsubscribe":
		on := in.Type == "subscribe"
		switch in.Channel {
		case "school":
			if !p.Role.IsWeb() {
				a.wsReply(c, wsOut{Type: "error", Code: "forbidden", Message: "Only school staff can watch the whole school."})
				return
			}
			c.SubscribeSchool(on)
			out := wsOut{Type: in.Type + "d", Channel: "school"}
			if on {
				snap, err := a.Live.Snapshot(ctx, c.SchoolID)
				if err != nil {
					slog.Error("live snapshot", "err", err)
				}
				out.Snapshot = snap
			}
			a.wsReply(c, out)
		case "trip":
			if !on {
				c.SubscribeTrip(in.TripID, false)
				a.wsReply(c, wsOut{Type: "unsubscribed", Channel: "trip", TripID: in.TripID})
				return
			}
			status, err := a.authorizeTripWatch(ctx, p, c.SchoolID, in.TripID)
			if err != nil {
				code := "not_found"
				if !errors.Is(err, store.ErrNotFound) {
					code = "internal_error"
					slog.Error("authorize trip watch", "err", err)
				}
				a.wsReply(c, wsOut{Type: "error", Code: code, TripID: in.TripID, Message: "Trip not found."})
				return
			}
			c.SubscribeTrip(in.TripID, true)
			out := wsOut{Type: "subscribed", Channel: "trip", TripID: in.TripID, Status: status}
			if loc, err := a.Live.Last(ctx, in.TripID); err == nil && loc != nil {
				out.Snapshot = []tracking.BusLocation{*loc}
			}
			a.wsReply(c, out)
		default:
			a.wsReply(c, wsOut{Type: "error", Code: "bad_request", Message: "channel must be school or trip"})
		}
	default:
		a.wsReply(c, wsOut{Type: "error", Code: "bad_request", Message: "type must be subscribe, unsubscribe or ping"})
	}
}

// authorizeTripWatch returns the trip's status if the caller may watch it:
// staff → any trip of their school; driver → own trip; parent → a trip their
// linked child rides (rule 6). Anything else is ErrNotFound.
func (a *API) authorizeTripWatch(ctx context.Context, p *auth.Principal, schoolID, tripID string) (string, error) {
	if _, err := uuid.Parse(tripID); err != nil {
		return "", store.ErrNotFound
	}
	switch {
	case p.Role.IsWeb():
		t, err := store.GetTrip(ctx, a.Store.Pool, schoolID, tripID)
		if err != nil {
			return "", err
		}
		return t.Status, nil
	case p.Role == models.RoleDriver:
		d, err := store.GetDriverByUser(ctx, a.Store.Pool, p.UserID)
		if err != nil {
			return "", err
		}
		t, err := store.GetDriverTrip(ctx, a.Store.Pool, d.ID, tripID)
		if err != nil {
			return "", err
		}
		return t.Status, nil
	case p.Role == models.RoleParent:
		tripSchool, ok, err := store.ParentCanSeeTrip(ctx, a.Store.Pool, p.UserID, tripID)
		if err != nil {
			return "", err
		}
		if !ok || tripSchool != schoolID {
			return "", store.ErrNotFound
		}
		t, err := store.GetTrip(ctx, a.Store.Pool, schoolID, tripID)
		if err != nil {
			return "", err
		}
		return t.Status, nil
	}
	return "", store.ErrNotFound
}
