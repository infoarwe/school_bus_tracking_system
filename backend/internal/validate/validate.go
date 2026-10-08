// Package validate collects field errors for request validation.
package validate

import (
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"
)

type Errors map[string]string

type V struct{ errs Errors }

func New() *V { return &V{errs: Errors{}} }

// Check records msg for field when ok is false. The first error per field wins.
func (v *V) Check(ok bool, field, msg string) {
	if !ok {
		if _, exists := v.errs[field]; !exists {
			v.errs[field] = msg
		}
	}
}

func (v *V) Required(field, val string) {
	v.Check(strings.TrimSpace(val) != "", field, "is required")
}

func (v *V) MaxLen(field, val string, n int) {
	v.Check(utf8.RuneCountInString(val) <= n, field, "is too long")
}

// Email checks format when val is non-empty; combine with Required if mandatory.
func (v *V) Email(field, val string) {
	if val == "" {
		return
	}
	a, err := mail.ParseAddress(val)
	v.Check(err == nil && a.Address == val, field, "must be a valid email address")
}

func (v *V) OK() bool       { return len(v.errs) == 0 }
func (v *V) Errors() Errors { return v.errs }

var nonDigits = regexp.MustCompile(`\D`)

// NormalizeMobile turns an Indian mobile number into +91XXXXXXXXXX.
// Accepts "9876543210", "+91 98765 43210", "09876543210", "919876543210".
func NormalizeMobile(s string) (string, bool) {
	d := nonDigits.ReplaceAllString(s, "")
	switch {
	case len(d) == 12 && strings.HasPrefix(d, "91"):
		d = d[2:]
	case len(d) == 11 && strings.HasPrefix(d, "0"):
		d = d[1:]
	}
	if len(d) != 10 || d[0] < '6' {
		return "", false
	}
	return "+91" + d, true
}

// Mobile validates and normalizes *val in place when non-empty.
func (v *V) Mobile(field string, val *string) {
	if val == nil || *val == "" {
		return
	}
	n, ok := NormalizeMobile(*val)
	v.Check(ok, field, "must be a valid 10-digit mobile number")
	if ok {
		*val = n
	}
}

func (v *V) OneOf(field, val string, allowed ...string) {
	for _, a := range allowed {
		if val == a {
			return
		}
	}
	v.Check(false, field, "must be one of: "+strings.Join(allowed, ", "))
}
