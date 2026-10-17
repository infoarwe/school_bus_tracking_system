package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTopicSubscriptionIID: subscribe/unsubscribe go to Instance ID
// batchAdd/batchRemove with access_token_auth (without it Google answers 401
// to a service-account token), gone tokens come back as failed, and other
// errors are returned without leaking the token.
func TestTopicSubscriptionIID(t *testing.T) {
	var gotPath, gotAuthHeader string
	var gotBody struct {
		To     string   `json:"to"`
		Tokens []string `json:"registration_tokens"`
	}
	reply := `{"results":[{},{"error":"NOT_FOUND"},{"error":"INVALID_ARGUMENT"}]}`
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuthHeader = r.URL.Path, r.Header.Get("access_token_auth")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	defer srv.Close()
	s := &FCMSender{client: srv.Client(), iidBase: srv.URL + "/iid/v1"}
	ctx := context.Background()

	failed, err := s.SubscribeTokens(ctx, []string{"ok", "gone", "bad"}, "school_x")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/iid/v1:batchAdd" || gotAuthHeader != "true" || gotBody.To != "/topics/school_x" || len(gotBody.Tokens) != 3 {
		t.Fatalf("request: path %q access_token_auth %q body %+v", gotPath, gotAuthHeader, gotBody)
	}
	if strings.Join(failed, ",") != "gone,bad" {
		t.Fatalf("failed = %v, want [gone bad]", failed)
	}

	reply = `{"results":[{}]}`
	if _, err := s.UnsubscribeTokens(ctx, []string{"ok"}, "school_x"); err != nil || gotPath != "/iid/v1:batchRemove" {
		t.Fatalf("unsubscribe: err %v path %q", err, gotPath)
	}

	// A transient per-token error is an error (retry later), not a gone token.
	reply = `{"results":[{"error":"INTERNAL"}]}`
	if failed, err := s.SubscribeTokens(ctx, []string{"ok"}, "school_x"); err == nil || len(failed) != 0 {
		t.Fatalf("INTERNAL: failed %v err %v, want error and no failed tokens", failed, err)
	}

	// HTTP errors are returned with the token redacted.
	status, reply = http.StatusForbidden, `{"error":"denied for secret-token-123"}`
	_, err = s.SubscribeTokens(ctx, []string{"secret-token-123"}, "school_x")
	if err == nil || strings.Contains(err.Error(), "secret-token-123") || !strings.Contains(err.Error(), "403") {
		t.Fatalf("403: err = %v, want a 403 error without the token", err)
	}
}
