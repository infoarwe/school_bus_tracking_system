// Command loadtest drives the tracking path at scale (S9-03): N buses each send a
// GPS point every interval while M parents watch their child's trip over the
// WebSocket. It reports GPS upload latency and live-update delivery latency.
//
// It CREATES DATA (routes, buses, drivers, trips, students, parents) in the school of
// the admin it logs in as. Run it only against a throwaway stack (see deploy/README.md,
// "Load test"), with OTP_DEV_CODE set and rate limits raised or off (all traffic comes
// from one IP):
//
//	go run ./cmd/loadtest -api https://localhost:8443 -insecure -buses 50 -parents 300 -duration 3m -yes
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	_ "time/tzdata"

	"github.com/coder/websocket"
)

type options struct {
	api, adminEmail, adminPassword, otp string
	buses, parents                      int
	duration, interval                  time.Duration
	targetP95                           time.Duration
	insecure, yes                       bool
}

func main() {
	var o options
	flag.StringVar(&o.api, "api", "https://localhost:8443", "API origin")
	flag.StringVar(&o.adminEmail, "admin-email", "admin@demo.local", "School Admin login (from cmd/seed)")
	flag.StringVar(&o.adminPassword, "admin-password", "Admin@12345", "School Admin password")
	flag.StringVar(&o.otp, "otp", "123456", "the API's OTP_DEV_CODE")
	flag.IntVar(&o.buses, "buses", 50, "buses (each a driver with a started trip)")
	flag.IntVar(&o.parents, "parents", 300, "parents watching a trip over the WebSocket")
	flag.DurationVar(&o.duration, "duration", 3*time.Minute, "how long buses send GPS")
	flag.DurationVar(&o.interval, "interval", 5*time.Second, "time between a bus's GPS uploads")
	flag.DurationVar(&o.targetP95, "target-p95", 300*time.Millisecond, "pass if the GPS upload p95 is below this")
	flag.BoolVar(&o.insecure, "insecure", false, "accept a self-signed TLS certificate (local test stack only)")
	flag.BoolVar(&o.yes, "yes", false, "confirm that test data may be created on this server")
	flag.Parse()
	if !o.yes {
		fmt.Fprintln(os.Stderr, "loadtest creates test data in the admin's school. Run it only on a throwaway stack, and pass -yes.")
		os.Exit(2)
	}
	ok, err := run(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
	if !ok {
		os.Exit(3)
	}
}

// --- HTTP client -------------------------------------------------------------

type client struct {
	base string
	http *http.Client
	// retry429: wait and retry when rate limited (set-up only; the measured phase counts 429s as errors).
	retry429 bool
}

type apiError struct {
	Status     int
	Body       string
	RetryAfter string
}

func (e *apiError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body) }

func (c *client) call(ctx context.Context, method, path, token string, body, out any) error {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		err := c.callOnce(ctx, method, path, token, body, out)
		var ae *apiError
		if !c.retry429 || !errors.As(err, &ae) || ae.Status != http.StatusTooManyRequests || time.Now().After(deadline) {
			return err
		}
		wait := time.Second
		if s, convErr := strconv.Atoi(ae.RetryAfter); convErr == nil && s > 0 {
			wait = time.Duration(s) * time.Second
		}
		time.Sleep(wait)
	}
}

func (c *client) callOnce(ctx context.Context, method, path, token string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return &apiError{res.StatusCode, strings.TrimSpace(string(raw)), res.Header.Get("Retry-After")}
	}
	if out != nil {
		var env struct{ Data json.RawMessage }
		if err := json.Unmarshal(raw, &env); err != nil {
			return err
		}
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

type idResp struct {
	ID string `json:"id"`
}

func (c *client) create(ctx context.Context, path, token string, body any) (string, error) {
	var r idResp
	err := c.call(ctx, "POST", path, token, body, &r)
	return r.ID, err
}

func (c *client) otpLogin(ctx context.Context, mobile, app, code string) (string, error) {
	if err := c.call(ctx, "POST", "/api/v1/auth/otp/send", "", map[string]string{"mobile": mobile, "app": app}, nil); err != nil {
		return "", err
	}
	var t struct {
		AccessToken string `json:"access_token"`
	}
	err := c.call(ctx, "POST", "/api/v1/auth/otp/verify", "", map[string]string{"mobile": mobile, "app": app, "code": code}, &t)
	return t.AccessToken, err
}

// parallel runs fn(i) for i in [0,n) on a few workers and returns the first error.
func parallel(n, workers int, fn func(i int) error) error {
	var wg sync.WaitGroup
	var first error
	var once sync.Once
	next := atomic.Int64{}
	for range workers {
		wg.Go(func() {
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				if err := fn(i); err != nil {
					once.Do(func() { first = fmt.Errorf("item %d: %w", i, err) })
					return
				}
			}
		})
	}
	wg.Wait()
	return first
}

// --- set-up ------------------------------------------------------------------

type bus struct {
	tripID, token string
	lat, lng      float64
	uploads       int // written only by this bus's goroutine
}

type parent struct{ tripID, token string }

func setup(ctx context.Context, c *client, o options) ([]*bus, []*parent, error) {
	var login struct {
		AccessToken string `json:"access_token"`
		User        struct {
			SchoolID *string `json:"school_id"`
		} `json:"user"`
	}
	if err := c.call(ctx, "POST", "/api/v1/auth/login", "", map[string]string{"email": o.adminEmail, "password": o.adminPassword}, &login); err != nil {
		return nil, nil, fmt.Errorf("admin login: %w", err)
	}
	if login.User.SchoolID == nil {
		return nil, nil, errors.New("use a School Admin login (Super Admin has no school)")
	}
	admin, base := login.AccessToken, "/api/v1/schools/"+*login.User.SchoolID
	var school struct {
		Timezone string `json:"timezone"`
	}
	if err := c.call(ctx, "GET", base, admin, nil, &school); err != nil {
		return nil, nil, err
	}
	loc, err := time.LoadLocation(school.Timezone)
	if err != nil {
		return nil, nil, err
	}
	today := time.Now().In(loc).Format("2006-01-02")
	run := time.Now().UnixMilli() % 10000 // distinct codes and mobiles per run

	buses := make([]*bus, o.buses)
	stops := make([][]string, o.buses)
	err = parallel(o.buses, 8, func(i int) error {
		code := fmt.Sprintf("LT%04d-%03d", run, i)
		routeID, err := c.create(ctx, base+"/routes", admin, map[string]any{"name": "Load " + code, "code": code})
		if err != nil {
			return fmt.Errorf("route: %w", err)
		}
		lat, lng := 11.0+float64(i)*0.01, 76.9
		for s := range 3 {
			id, err := c.create(ctx, base+"/routes/"+routeID+"/stops", admin, map[string]any{
				"name": fmt.Sprintf("Stop %d", s+1), "latitude": lat + 0.01*float64(s+1), "longitude": lng})
			if err != nil {
				return fmt.Errorf("stop: %w", err)
			}
			stops[i] = append(stops[i], id)
		}
		busID, err := c.create(ctx, base+"/buses", admin, map[string]any{"vehicle_number": "TN-" + code, "capacity": 40})
		if err != nil {
			return fmt.Errorf("bus: %w", err)
		}
		mobile := fmt.Sprintf("8%04d%05d", run, i)
		driverID, err := c.create(ctx, base+"/drivers", admin, map[string]any{"name": "Driver " + code, "mobile": mobile})
		if err != nil {
			return fmt.Errorf("driver: %w", err)
		}
		tripID, err := c.create(ctx, base+"/trips", admin, map[string]any{"trip_date": today, "trip_type": "morning_pickup",
			"route_id": routeID, "bus_id": busID, "driver_id": driverID})
		if err != nil {
			return fmt.Errorf("trip: %w", err)
		}
		tok, err := c.otpLogin(ctx, mobile, "driver", o.otp)
		if err != nil {
			return fmt.Errorf("driver login: %w", err)
		}
		for _, step := range []string{"confirm", "start"} {
			if err := c.call(ctx, "POST", "/api/v1/driver/trips/"+tripID+"/"+step, tok, nil, nil); err != nil {
				return fmt.Errorf("trip %s: %w", step, err)
			}
		}
		buses[i] = &bus{tripID: tripID, token: tok, lat: lat, lng: lng}
		stops[i] = append(stops[i], routeID) // last element: the route ID
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	fmt.Printf("set-up: %d buses with started trips\n", o.buses)

	parents := make([]*parent, o.parents)
	err = parallel(o.parents, 8, func(i int) error {
		b := i % o.buses
		routeStops := stops[b]
		code := fmt.Sprintf("LT%04d-P%04d", run, i)
		studentID, err := c.create(ctx, base+"/students", admin, map[string]any{"admission_no": code, "name": "Child " + code})
		if err != nil {
			return fmt.Errorf("student: %w", err)
		}
		stop := routeStops[i%3]
		if err := c.call(ctx, "PUT", base+"/students/"+studentID+"/assignment", admin, map[string]any{
			"route_id": routeStops[3], "pickup_stop_id": stop, "drop_stop_id": stop}, nil); err != nil {
			return fmt.Errorf("assignment: %w", err)
		}
		mobile := fmt.Sprintf("7%04d%05d", run, i)
		if _, err := c.create(ctx, base+"/parents", admin, map[string]any{"name": "Parent " + code, "mobile": mobile,
			"children": []map[string]string{{"student_id": studentID}}}); err != nil {
			return fmt.Errorf("parent: %w", err)
		}
		tok, err := c.otpLogin(ctx, mobile, "parent", o.otp)
		if err != nil {
			return fmt.Errorf("parent login: %w", err)
		}
		parents[i] = &parent{tripID: buses[b].tripID, token: tok}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	fmt.Printf("set-up: %d parents\n", o.parents)
	return buses, parents, nil
}

// --- the test ----------------------------------------------------------------

type samples struct {
	mu sync.Mutex
	d  []time.Duration
}

func (s *samples) add(d time.Duration) { s.mu.Lock(); s.d = append(s.d, d); s.mu.Unlock() }

func (s *samples) pct(p float64) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.d) == 0 {
		return 0
	}
	d := slices.Clone(s.d)
	slices.Sort(d)
	return d[int(math.Ceil(p/100*float64(len(d))))-1]
}

func run(o options) (bool, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = o.buses + 16
	if o.insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // -insecure: local self-signed test stack only
	}
	c := &client{base: strings.TrimRight(o.api, "/"), http: &http.Client{Transport: transport, Timeout: 30 * time.Second}}
	ctx := context.Background()

	buses, parents, err := setup(ctx, &client{base: c.base, http: c.http, retry429: true}, o)
	if err != nil {
		return false, err
	}

	var upload, delivery samples
	var uploadErrors, wsErrors, wsMessages, connected atomic.Int64
	sent := sync.Map{} // tripID|recorded_at(ms) → client send time

	runCtx, stop := context.WithTimeout(ctx, o.duration+15*time.Second)
	defer stop()
	var wg sync.WaitGroup

	wsURL := "ws" + strings.TrimPrefix(c.base, "http") + "/api/v1/ws?access_token="
	for _, p := range parents {
		wg.Go(func() {
			conn, _, err := websocket.Dial(runCtx, wsURL+p.token, &websocket.DialOptions{HTTPClient: c.http})
			if err != nil {
				if wsErrors.Add(1) <= 3 {
					fmt.Fprintln(os.Stderr, "websocket dial error:", err)
				}
				return
			}
			defer conn.CloseNow()
			conn.SetReadLimit(1 << 20)
			sub, _ := json.Marshal(map[string]string{"type": "subscribe", "channel": "trip", "trip_id": p.tripID})
			if err := conn.Write(runCtx, websocket.MessageText, sub); err != nil {
				wsErrors.Add(1)
				return
			}
			connected.Add(1)
			for {
				_, data, err := conn.Read(runCtx)
				if err != nil {
					return
				}
				var m struct {
					Type     string `json:"type"`
					TripID   string `json:"trip_id"`
					Location struct {
						RecordedAt time.Time `json:"recorded_at"`
					} `json:"location"`
				}
				if json.Unmarshal(data, &m) != nil || m.Type != "location" {
					continue
				}
				wsMessages.Add(1)
				if t, ok := sent.Load(fmt.Sprintf("%s|%d", m.TripID, m.Location.RecordedAt.UnixMilli())); ok {
					delivery.add(time.Since(t.(time.Time)))
				}
			}
		})
	}
	time.Sleep(3 * time.Second) // let the sockets connect and subscribe
	fmt.Printf("websockets: %d connected, %d failed\n", connected.Load(), wsErrors.Load())

	start := time.Now()
	var bwg sync.WaitGroup
	for i, b := range buses {
		bwg.Go(func() {
			// Spread the buses over the interval, like real phones.
			time.Sleep(time.Duration(i) * o.interval / time.Duration(len(buses)))
			tick := time.NewTicker(o.interval)
			defer tick.Stop()
			for step := 0; time.Since(start) < o.duration; step++ {
				now := time.Now().UTC().Truncate(time.Millisecond)
				lat := b.lat + float64(step)*0.0004 // about 45 m per 5 s ≈ 32 km/h
				sent.Store(fmt.Sprintf("%s|%d", b.tripID, now.UnixMilli()), time.Now())
				t0 := time.Now()
				err := c.call(runCtx, "POST", "/api/v1/driver/trips/"+b.tripID+"/locations", b.token, map[string]any{
					"points": []map[string]any{{"latitude": lat, "longitude": b.lng, "accuracy_m": 8, "speed_mps": 9,
						"recorded_at": now.Format(time.RFC3339Nano)}}}, nil)
				b.uploads++
				if err == nil {
					upload.add(time.Since(t0)) // latency of successful uploads only
				} else {
					uploadErrors.Add(1)
					if uploadErrors.Load() <= 3 {
						fmt.Fprintln(os.Stderr, "upload error:", err)
					}
				}
				select {
				case <-tick.C:
				case <-runCtx.Done():
					return
				}
			}
		})
	}
	bwg.Wait()
	elapsed := time.Since(start).Seconds()
	time.Sleep(2 * time.Second) // last deliveries
	stop()
	wg.Wait()

	n := len(upload.d)
	minPer, maxPer := math.MaxInt, 0
	for _, b := range buses {
		minPer, maxPer = min(minPer, b.uploads), max(maxPer, b.uploads)
	}
	fmt.Printf("measured phase: %.1fs; uploads per bus: min %d, max %d\n", elapsed, minPer, maxPer)
	fmt.Printf("\n== Load test: %d buses every %s, %d parent WebSockets, %s ==\n", len(buses), o.interval, len(parents), o.duration)
	fmt.Printf("GPS uploads: %d (%.1f/s), errors %d\n", n, float64(n)/elapsed, uploadErrors.Load())
	fmt.Printf("  latency p50 %s  p95 %s  p99 %s  max %s\n", upload.pct(50), upload.pct(95), upload.pct(99), upload.pct(100))
	fmt.Printf("Live updates delivered to parents: %d (connected %d, failed %d)\n", wsMessages.Load(), connected.Load(), wsErrors.Load())
	fmt.Printf("  driver send → parent receive p50 %s  p95 %s  p99 %s\n", delivery.pct(50), delivery.pct(95), delivery.pct(99))

	pass := n > 0 && uploadErrors.Load() == 0 && wsErrors.Load() == 0 && upload.pct(95) < o.targetP95
	fmt.Printf("Target: GPS upload p95 < %s, no errors → %s\n", o.targetP95, map[bool]string{true: "PASS", false: "FAIL"}[pass])
	return pass, nil
}
