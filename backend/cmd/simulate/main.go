// Command simulate drives a bus along a route through the real API, as the
// Driver app would: log in with OTP, confirm and start today's trip, then post
// GPS points moving from stop to stop. For testing live tracking without a phone.
//
//	go run ./cmd/simulate                          # demo driver, today's first open trip
//	go run ./cmd/simulate -speed 60 -interval 2s -end
//	go run ./cmd/simulate -mobile 9000000005 -trip <trip-id> -noise
//
// Needs OTP_DEV_CODE on the API (dev/staging only).
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"time"
)

type stop struct {
	Name      string  `json:"name"`
	Sequence  int     `json:"sequence"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type trip struct {
	ID       string `json:"id"`
	TripType string `json:"trip_type"`
	Status   string `json:"status"`
	Route    struct {
		Code string `json:"code"`
	} `json:"route"`
	Bus struct {
		VehicleNumber string `json:"vehicle_number"`
	} `json:"bus"`
	Stops []stop `json:"stops"`
}

func main() {
	api := flag.String("api", "http://localhost:8085", "API base URL")
	mobile := flag.String("mobile", "9000000001", "driver mobile number")
	otp := flag.String("otp", "123456", "OTP (the API's OTP_DEV_CODE)")
	tripID := flag.String("trip", "", "trip ID (default: today's first trip that is not finished)")
	speedKmh := flag.Float64("speed", 30, "bus speed in km/h")
	interval := flag.Duration("interval", 3*time.Second, "time between GPS points")
	dwell := flag.Duration("dwell", 20*time.Second, "time spent at each stop")
	noise := flag.Bool("noise", false, "now and then send a bad point (poor accuracy or a jump) to exercise the filter")
	end := flag.Bool("end", false, "end the trip after the last stop")
	flag.Parse()

	c := &client{base: *api + "/api/v1"}
	if err := c.login(*mobile, *otp); err != nil {
		log.Fatalf("login: %v", err)
	}

	t, err := c.pickTrip(*tripID)
	if err != nil {
		log.Fatal(err)
	}
	for _, action := range []string{"confirm", "start"} {
		if (action == "confirm" && t.Status == "scheduled") || (action == "start" && t.Status == "confirmed") {
			if err := c.do("POST", "/driver/trips/"+t.ID+"/"+action, nil, t); err != nil {
				log.Fatalf("%s trip: %v", action, err)
			}
			log.Printf("trip %s", t.Status)
		}
	}
	if t.Status != "started" {
		log.Fatalf("trip is %s; cannot send GPS", t.Status)
	}

	stops := t.Stops
	if t.TripType == "evening_drop" { // drop the children in reverse order
		for i, j := 0, len(stops)-1; i < j; i, j = i+1, j-1 {
			stops[i], stops[j] = stops[j], stops[i]
		}
	}
	log.Printf("driving %s (%s, bus %s) through %d stops at %.0f km/h", t.Route.Code, t.TripType, t.Bus.VehicleNumber, len(stops), *speedKmh)

	step := *speedKmh / 3.6 * interval.Seconds() // metres per point
	sent := 0
	send := func(lat, lng float64) {
		p := map[string]any{"latitude": lat, "longitude": lng, "accuracy_m": 5 + rand.Float64()*10,
			"speed_mps": *speedKmh / 3.6, "recorded_at": time.Now().UTC().Format(time.RFC3339Nano)}
		if *noise && rand.IntN(10) == 0 {
			if rand.IntN(2) == 0 {
				p["accuracy_m"] = 400.0 // poor fix
			} else {
				p["latitude"] = lat + 0.5 // jump of ~55 km
			}
		}
		var res struct {
			Accepted int `json:"accepted"`
			Rejected []struct {
				Reason string `json:"reason"`
			} `json:"rejected"`
		}
		if err := c.do("POST", "/driver/trips/"+t.ID+"/locations", map[string]any{"points": []any{p}}, &res); err != nil {
			log.Printf("send failed: %v", err)
			return
		}
		sent++
		if len(res.Rejected) > 0 {
			log.Printf("point rejected by the server: %s", res.Rejected[0].Reason)
		}
		time.Sleep(*interval)
	}

	for i := 0; i+1 < len(stops); i++ {
		a, b := stops[i], stops[i+1]
		log.Printf("at stop %d %s", a.Sequence, a.Name)
		for waited := time.Duration(0); waited < *dwell; waited += *interval {
			send(a.Latitude, a.Longitude)
		}
		n := int(math.Max(1, math.Ceil(distanceM(a.Latitude, a.Longitude, b.Latitude, b.Longitude)/step)))
		for k := 1; k <= n; k++ {
			f := float64(k) / float64(n)
			send(a.Latitude+(b.Latitude-a.Latitude)*f, a.Longitude+(b.Longitude-a.Longitude)*f)
		}
	}
	last := stops[len(stops)-1]
	log.Printf("at last stop %d %s (%d points sent)", last.Sequence, last.Name, sent)
	for waited := time.Duration(0); waited < *dwell; waited += *interval {
		send(last.Latitude, last.Longitude)
	}

	// Morning Pickup ends at the school, if the school has set its location.
	var prog struct {
		Progress *struct {
			Stops []stop `json:"stops"`
		} `json:"progress"`
	}
	if err := c.do("GET", "/driver/trips/"+t.ID+"/progress", nil, &prog); err == nil && prog.Progress != nil {
		if ps := prog.Progress.Stops; len(ps) > 0 && ps[len(ps)-1].Name == "School" {
			school := ps[len(ps)-1]
			n := int(math.Max(1, math.Ceil(distanceM(last.Latitude, last.Longitude, school.Latitude, school.Longitude)/step)))
			log.Printf("driving to school")
			for k := 1; k <= n+2; k++ {
				f := math.Min(1, float64(k)/float64(n))
				send(last.Latitude+(school.Latitude-last.Latitude)*f, last.Longitude+(school.Longitude-last.Longitude)*f)
			}
			log.Printf("at school")
		}
	}

	if *end {
		if err := c.do("POST", "/driver/trips/"+t.ID+"/end", nil, t); err != nil {
			log.Fatalf("end trip: %v", err)
		}
		log.Printf("trip %s", t.Status)
	}
}

// client is a minimal Driver-app API client with token refresh.
type client struct {
	base            string
	access, refresh string
}

func (c *client) login(mobile, otp string) error {
	if err := c.do("POST", "/auth/otp/send", map[string]string{"mobile": mobile, "app": "driver"}, nil); err != nil {
		var ae *apiError
		if !errors.As(err, &ae) || ae.Code != "otp_too_soon" { // a code from a previous run is still valid
			return err
		}
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.do("POST", "/auth/otp/verify", map[string]string{"mobile": mobile, "app": "driver", "code": otp, "device_name": "GPS simulator"}, &tok); err != nil {
		return err
	}
	c.access, c.refresh = tok.AccessToken, tok.RefreshToken
	return nil
}

func (c *client) pickTrip(id string) (*trip, error) {
	if id != "" {
		var t trip
		return &t, c.do("GET", "/driver/trips/"+id, nil, &t)
	}
	var trips []trip
	if err := c.do("GET", "/driver/trips", nil, &trips); err != nil {
		return nil, err
	}
	for i := range trips {
		if trips[i].Status != "completed" && trips[i].Status != "cancelled" {
			return &trips[i], nil
		}
	}
	return nil, errors.New("no open trip for this driver today: assign one on the Daily Trips page")
}

type apiError struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

func (c *client) do(method, path string, body, out any) error {
	err := c.once(method, path, body, out)
	var ae *apiError
	if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized && c.refresh != "" {
		var tok struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}
		r := c.refresh
		c.refresh = "" // avoid loops
		if err := c.once("POST", "/auth/refresh", map[string]string{"refresh_token": r}, &tok); err != nil {
			return err
		}
		c.access, c.refresh = tok.AccessToken, tok.RefreshToken
		return c.once(method, path, body, out)
	}
	return err
}

func (c *client) once(method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.access != "" {
		req.Header.Set("Authorization", "Bearer "+c.access)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var env struct {
		Data  json.RawMessage `json:"data"`
		Error *apiError       `json:"error"`
	}
	_ = json.NewDecoder(res.Body).Decode(&env)
	if res.StatusCode >= 300 {
		if env.Error == nil {
			env.Error = &apiError{}
		}
		env.Error.Status = res.StatusCode
		return env.Error
	}
	if out != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

func distanceM(lat1, lng1, lat2, lng2 float64) float64 {
	rad := math.Pi / 180
	dLat, dLng := (lat2-lat1)*rad, (lng2-lng1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * 6371000 * math.Asin(math.Sqrt(a))
}

func init() { log.SetOutput(os.Stdout); log.SetFlags(log.Ltime) }
