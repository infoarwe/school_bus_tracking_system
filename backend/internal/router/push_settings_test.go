package router_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/notify"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
)

// serviceAccountJSON builds a realistic Firebase key file with a fresh RSA key.
func serviceAccountJSON(t *testing.T, projectID string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	raw, _ := json.Marshal(map[string]string{
		"type":         "service_account",
		"project_id":   projectID,
		"client_email": "firebase-adminsdk@" + projectID + ".iam.gserviceaccount.com",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"token_uri":    "https://oauth2.googleapis.com/token",
	})
	return string(raw)
}

func TestPushKeyPerSchool(t *testing.T) {
	e := newEnv(t)
	a, b := e.school("AAA"), e.school("BBB")
	e.user(&a, models.RoleSchoolAdmin, "admin@a.test", "")
	e.user(&a, models.RoleTransportManager, "tm@a.test", "")
	e.user(&b, models.RoleSchoolAdmin, "admin@b.test", "")
	admin, tm, other := e.login("admin@a.test"), e.login("tm@a.test"), e.login("admin@b.test")
	url := "/api/v1/schools/" + a + "/settings/push"

	// Clear reasons for bad files.
	for _, bad := range []string{"not json", `{"type":"authorized_user"}`, `{"type":"service_account","project_id":"p","client_email":"x@y","private_key":"nope"}`} {
		e.expect(e.do("PUT", url, admin, map[string]string{"service_account_json": bad}), 400, "validation_failed")
	}

	key := serviceAccountJSON(t, "dps-bus-app")
	r := e.do("PUT", url, admin, map[string]string{"service_account_json": key})
	e.expect(r, 200, "")
	if r.data()["configured"] != true || r.data()["project_id"] != "dps-bus-app" {
		t.Fatalf("upload = %v", r.data())
	}
	got := e.do("GET", url, admin, nil)
	if strings.Contains(stringify(got.Body), "PRIVATE KEY") || strings.Contains(stringify(r.Body), "PRIVATE KEY") {
		t.Fatal("the key must never be returned")
	}
	var stored []byte
	var audit string
	_ = e.pool.QueryRow(context.Background(), `SELECT fcm_credentials_enc FROM schools WHERE id = $1`, a).Scan(&stored)
	_ = e.pool.QueryRow(context.Background(), `SELECT after::text FROM audit_logs WHERE action = 'settings.push_key_upload'`).Scan(&audit)
	if strings.Contains(string(stored), "PRIVATE KEY") || strings.Contains(audit, "PRIVATE KEY") {
		t.Fatal("the key must be encrypted at rest and kept out of the audit log")
	}
	if test := e.do("POST", url+"/test", admin, nil); test.data()["ok"] != true {
		t.Errorf("test = %v", test.data())
	}

	// Admins of this school only.
	e.expect(e.do("GET", url, tm, nil), 403, "forbidden")
	e.expect(e.do("PUT", url, tm, map[string]string{"service_account_json": key}), 403, "forbidden")
	e.expect(e.do("GET", url, other, nil), 404, "not_found")
	if g := e.do("GET", "/api/v1/schools/"+b+"/settings/push", other, nil).data(); g["configured"] != false {
		t.Errorf("school B should have no key: %v", g)
	}

	// Each school's pushes go through its own Firebase project; others use the fallback.
	type tagged struct {
		notify.Sender
		project string
	}
	fallback := &fakePush{}
	built := 0
	senders := &notify.SchoolSenders{Store: store.New(e.pool), Secrets: testSecrets(t), Fallback: fallback,
		NewSender: func(_ context.Context, raw []byte) (notify.Sender, error) {
			built++
			sa, err := notify.ParseServiceAccount(raw)
			if err != nil {
				return nil, err
			}
			return tagged{&fakePush{}, sa.ProjectID}, nil
		}}
	sa, _ := senders.For(context.Background(), a)
	if s, ok := sa.(tagged); !ok || s.project != "dps-bus-app" {
		t.Fatalf("school A sender = %#v", sa)
	}
	if sb, _ := senders.For(context.Background(), b); sb != notify.Sender(fallback) {
		t.Errorf("school B should use the fallback, got %#v", sb)
	}
	senders.For(context.Background(), a)
	if built != 1 {
		t.Errorf("sender should be cached, built %d times", built)
	}
	// A new key replaces the cached sender.
	e.expect(e.do("PUT", url, admin, map[string]string{"service_account_json": serviceAccountJSON(t, "dps-bus-app-v2")}), 200, "")
	if s, _ := senders.For(context.Background(), a); s.(tagged).project != "dps-bus-app-v2" {
		t.Errorf("after re-upload: %#v", s)
	}

	// Removing it falls back again.
	del := e.do("DELETE", url, admin, nil)
	if del.data()["configured"] != false {
		t.Errorf("delete = %v", del.data())
	}
	if s, _ := senders.For(context.Background(), a); s != notify.Sender(fallback) {
		t.Error("removed key should fall back")
	}
	e.expect(e.do("POST", url+"/test", admin, nil), 409, "push_not_configured")
}
