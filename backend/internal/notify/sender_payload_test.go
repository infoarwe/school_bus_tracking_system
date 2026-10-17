package notify

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestFCMMessagePayloadIsTopicOnly: the outgoing FCM HTTP v1 body must carry
// "topic" and must never carry "token" — the one hard requirement the whole
// migration exists to satisfy. Tested against the pure, network-free
// buildFCMMessage (no oauth2/HTTP/Firebase involved), not the real Send,
// specifically so this needs no real service account or network access.
func TestFCMMessagePayloadIsTopicOnly(t *testing.T) {
	raw, err := buildFCMMessage(Push{
		Topic: "school_11111111-1111-1111-1111-111111111111_route_22222222-2222-2222-2222-222222222222",
		Title: "Bus started", Body: "The Morning Pickup bus has started its route.",
		Data: map[string]string{"type": "bus_started", "trip_id": "t1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"token"`) {
		t.Fatalf("payload must never contain a token field: %s", raw)
	}
	if !strings.Contains(string(raw), `"topic"`) {
		t.Fatalf("payload must contain a topic field: %s", raw)
	}

	var parsed struct {
		Message struct {
			Topic string            `json:"topic"`
			Token string            `json:"token"`
			Data  map[string]string `json:"data"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Message.Topic == "" {
		t.Error("message.topic is empty")
	}
	if parsed.Message.Token != "" {
		t.Errorf("message.token should not even be a field in the schema we send, got %q", parsed.Message.Token)
	}
}

func TestFCMMessageRejectsEmptyTopic(t *testing.T) {
	if _, err := buildFCMMessage(Push{Title: "x", Body: "y"}); err == nil {
		t.Error("a Push with no Topic must be rejected, not silently sent somewhere")
	}
}
