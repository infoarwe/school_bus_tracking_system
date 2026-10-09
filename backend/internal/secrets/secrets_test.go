package secrets

import (
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	b, err := New(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := b.Encrypt("AIzaSyExampleExampleExampleExample12345")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "AIza") {
		t.Fatal("ciphertext contains the plain text")
	}
	got, err := b.Decrypt(sealed)
	if err != nil || got != "AIzaSyExampleExampleExampleExample12345" {
		t.Fatalf("got %q, %v", got, err)
	}

	other, _ := New(strings.Repeat("cd", 32))
	if _, err := other.Decrypt(sealed); err == nil {
		t.Fatal("decrypting with another key must fail")
	}
	if _, err := New("short"); err == nil {
		t.Fatal("short key must be rejected")
	}
}
