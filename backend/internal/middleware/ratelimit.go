package middleware

import (
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/ratelimit"
)

// RuleOf picks a rule from the limiter's current rules.
type RuleOf func(*ratelimit.Rules) ratelimit.Rule

// RateLimit refuses requests over the rule for the key keyOf returns ("" = not
// limited) with 429 rate_limited and a Retry-After header. A nil limiter is off.
func RateLimit(l *ratelimit.Limiter, rule RuleOf, keyOf func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if l == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !Allow(w, r, l, rule(&l.Rules), keyOf(r)) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Allow counts one request for id; when over the limit it writes the 429 and
// returns false. Handlers use it for limits keyed by the request body (email, mobile).
func Allow(w http.ResponseWriter, r *http.Request, l *ratelimit.Limiter, rule ratelimit.Rule, id string) bool {
	if l == nil {
		return true
	}
	res, err := l.Allow(r.Context(), rule, id)
	if err != nil {
		slog.Warn("rate limit check failed; allowing request", "rule", rule.Name, "err", err)
	}
	if res.Allowed {
		return true
	}
	secs := int(math.Ceil(res.RetryAfter.Seconds()))
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	httpx.Error(w, http.StatusTooManyRequests, "rate_limited",
		fmt.Sprintf("Too many requests. Please try again in %s.", humanSeconds(secs)))
	return false
}

func humanSeconds(s int) string {
	if s < 90 {
		return fmt.Sprintf("%d seconds", s)
	}
	return fmt.Sprintf("%d minutes", (s+59)/60)
}

// ClientIP is the caller's IP without the port. Behind Nginx it is the real client
// IP only when TRUST_PROXY_HEADERS is on (chi's RealIP rewrites RemoteAddr).
func ClientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// UserKey is the authenticated caller's user ID ("" before Authenticate).
func UserKey(r *http.Request) string {
	if p := auth.FromContext(r.Context()); p != nil {
		return p.UserID
	}
	return ""
}
