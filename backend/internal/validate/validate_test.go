package validate

import "testing"

func TestNormalizeMobile(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"9876543210", "+919876543210", true},
		{"+91 98765 43210", "+919876543210", true},
		{"09876543210", "+919876543210", true},
		{"919876543210", "+919876543210", true},
		{"12345", "", false},
		{"1234567890", "", false}, // Indian mobiles start with 6-9
	}
	for _, tt := range tests {
		got, ok := NormalizeMobile(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("NormalizeMobile(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
