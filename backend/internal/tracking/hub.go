package tracking

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// Hub fans out events from Redis pub/sub to the WebSocket clients connected to
// this API instance. Every instance runs its own hub, so clients on any
// instance receive every event of their school.
type Hub struct {
	live *Live

	mu      sync.RWMutex
	clients map[string]map[*Client]struct{} // schoolID → clients
}

func NewHub(live *Live) *Hub {
	return &Hub{live: live, clients: map[string]map[*Client]struct{}{}}
}

// Client is one WebSocket connection. Its subscriptions were authorized when made.
type Client struct {
	SchoolID string
	Send     chan []byte // buffered; the connection writer drains it
	Closed   chan struct{}

	mu        sync.Mutex
	allSchool bool            // admin live map: every trip of the school
	trips     map[string]bool // parent/driver/admin trip views
	closeOnce sync.Once
}

func NewClient(schoolID string) *Client {
	return &Client{SchoolID: schoolID, Send: make(chan []byte, 64), Closed: make(chan struct{}), trips: map[string]bool{}}
}

func (c *Client) SubscribeSchool(on bool) {
	c.mu.Lock()
	c.allSchool = on
	c.mu.Unlock()
}

func (c *Client) SubscribeTrip(tripID string, on bool) {
	c.mu.Lock()
	if on {
		c.trips[tripID] = true
	} else {
		delete(c.trips, tripID)
	}
	c.mu.Unlock()
}

// wants: staff-only events go to school-channel subscribers (only staff may
// subscribe to the school); trip events go to that trip's subscribers too.
func (c *Client) wants(ev Event) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ev.Audience == AudienceStaff {
		return c.allSchool
	}
	return c.allSchool || c.trips[ev.TripID]
}

// Close ends the client once; the connection handler watches Closed.
func (c *Client) Close() { c.closeOnce.Do(func() { close(c.Closed) }) }

func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[c.SchoolID] == nil {
		h.clients[c.SchoolID] = map[*Client]struct{}{}
	}
	h.clients[c.SchoolID][c] = struct{}{}
}

func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients[c.SchoolID], c)
	if len(h.clients[c.SchoolID]) == 0 {
		delete(h.clients, c.SchoolID)
	}
}

// Deliver sends an event to the school's clients that subscribed to it. A client
// too slow to keep up is disconnected rather than allowed to block everyone.
func (h *Hub) Deliver(schoolID string, ev Event, raw []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients[schoolID] {
		if !c.wants(ev) {
			continue
		}
		select {
		case c.Send <- raw:
		default:
			slog.Warn("websocket client too slow; disconnecting", "school_id", schoolID)
			c.Close()
		}
	}
}

// Run reads Redis pub/sub until ctx ends, reconnecting if Redis drops.
func (h *Hub) Run(ctx context.Context) {
	for ctx.Err() == nil {
		ps := h.live.subscribeEvents(ctx)
		stop := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				_ = ps.Close() // ends the range below
			case <-stop:
			}
		}()
		ch := ps.Channel()
		for msg := range ch {
			var ev Event
			if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
				continue
			}
			h.Deliver(schoolOfChannel(msg.Channel), ev, []byte(msg.Payload))
		}
		close(stop)
		_ = ps.Close()
		if ctx.Err() == nil {
			slog.Warn("redis events subscription ended; retrying")
			time.Sleep(2 * time.Second)
		}
	}
}
