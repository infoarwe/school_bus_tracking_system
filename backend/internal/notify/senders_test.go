package notify

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"testing"
)

func TestServiceAccountAndFCMSender(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	raw, _ := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "school-app", "client_email": "fcm@school-app.iam.gserviceaccount.com",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"token_uri":   "https://oauth2.googleapis.com/token",
	})
	sa, err := ParseServiceAccount(raw)
	if err != nil || sa.ProjectID != "school-app" {
		t.Fatalf("parse: %v %+v", err, sa)
	}
	s, err := NewFCMSenderJSON(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if s.endpoint != "https://fcm.googleapis.com/v1/projects/school-app/messages:send" {
		t.Errorf("endpoint = %s", s.endpoint)
	}
	if _, err := ParseServiceAccount([]byte(`{"type":"service_account"}`)); err == nil {
		t.Error("a key without project_id must be rejected")
	}
}
