package tracking

import (
	"math"
	"testing"
	"time"
)

func TestDistanceM(t *testing.T) {
	// Gandhipuram → Peelamedu, Coimbatore: about 4.1 km.
	d := DistanceM(11.0168, 76.9663, 11.0285, 77.0028)
	if math.Abs(d-4180) > 150 {
		t.Errorf("distance = %.0f m", d)
	}
}

func TestFilter(t *testing.T) {
	now := time.Date(2026, 10, 7, 2, 0, 0, 0, time.UTC)
	start := now.Add(-30 * time.Minute)
	at := func(minAgo float64) time.Time { return now.Add(-time.Duration(minAgo * float64(time.Minute))) }
	p := func(lat, lng, acc float64, ts time.Time) Point {
		return Point{Latitude: lat, Longitude: lng, AccuracyM: acc, RecordedAt: ts}
	}

	points := []Point{
		p(11.0200, 76.9700, 10, at(2)),                     // 0 ok (sent out of order)
		p(11.0168, 76.9663, 12, at(3)),                     // 1 ok, earliest
		p(11.0210, 76.9710, 500, at(1.5)),                  // 2 poor accuracy
		p(0, 0, 5, at(1.4)),                                // 3 no fix
		p(11.5000, 77.5000, 8, at(1.3)),                    // 4 jump of ~70 km in 42 s
		p(11.0220, 76.9720, 9, at(1)),                      // 5 ok: judged against point 0, not the jump
		p(11.0220, 76.9720, 9, at(1)),                      // 6 duplicate time
		p(11.0230, 76.9730, 9, now.Add(5*time.Minute)),     // 7 future
		p(11.0230, 76.9730, 9, start.Add(-10*time.Minute)), // 8 before trip start
	}
	kept, rejected := Filter(points, nil, start, now)

	if len(kept) != 3 || kept[0].RecordedAt != at(3) || kept[2].RecordedAt != at(1) {
		t.Fatalf("kept = %+v", kept)
	}
	want := map[int]string{
		2: "accuracy worse than 100 m",
		3: "no fix (0,0)",
		4: "jump faster than 150 km/h",
		6: "duplicate or out of order",
		7: "recorded_at is in the future",
		8: "recorded before the trip started",
	}
	if len(rejected) != len(want) {
		t.Fatalf("rejected = %+v", rejected)
	}
	for _, r := range rejected {
		if want[r.Index] != r.Reason {
			t.Errorf("point %d: got %q, want %q", r.Index, r.Reason, want[r.Index])
		}
	}

	// The previous batch's last point is the anchor for the next batch.
	last := kept[len(kept)-1]
	_, rej := Filter([]Point{p(11.0220, 76.9720, 9, at(1))}, &last, start, now)
	if len(rej) != 1 {
		t.Error("a point already received must be rejected")
	}
}
