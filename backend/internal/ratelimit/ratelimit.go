// Package ratelimit counts requests per key in Redis (fixed windows), so the
// limits hold across every API instance. If Redis fails, requests are allowed
// (and the error logged): rate limiting must never take the API down.
package ratelimit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Rule allows Limit requests per Window for one key (an IP, a user, an email…).
type Rule struct {
	Name   string
	Limit  int
	Window time.Duration
}

// Rules are the limits the API applies. Read on every request, so tests may change them.
type Rules struct {
	IP              Rule // every /api/v1 request, per client IP
	User            Rule // every authenticated request, per user
	Auth            Rule // login, 2FA, OTP send/verify and refresh, per client IP
	OTPSendIP       Rule // OTP codes requested per client IP (SMS cost / abuse)
	LoginAccount    Rule // password and 2FA attempts per account
	OTPVerifyMobile Rule // OTP verify attempts per mobile number and app
}

// DefaultRules are generous for normal use: many parents can share one mobile
// carrier IP (CGNAT), so per-IP limits are high and per-account limits strict.
func DefaultRules() Rules {
	return Rules{
		IP:              Rule{"ip", 1200, time.Minute},
		User:            Rule{"user", 600, time.Minute},
		Auth:            Rule{"auth", 60, 15 * time.Minute},
		OTPSendIP:       Rule{"otp_send_ip", 20, time.Hour},
		LoginAccount:    Rule{"login_account", 10, 15 * time.Minute},
		OTPVerifyMobile: Rule{"otp_verify_mobile", 10, 15 * time.Minute},
	}
}

// Limiter checks rules against counters in Redis.
type Limiter struct {
	rdb   redis.UniversalClient
	Rules Rules
}

func New(rdb redis.UniversalClient, rules Rules) *Limiter {
	return &Limiter{rdb: rdb, Rules: rules}
}

// Result of one check. RetryAfter is set when the request is refused.
type Result struct {
	Allowed    bool
	RetryAfter time.Duration
}

// INCR the window's counter, starting its expiry on the first hit; returns the
// count and the remaining time in ms. Atomic, so a crash cannot leave a counter without expiry.
var hit = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
local ttl = redis.call('PTTL', KEYS[1])
if ttl < 0 then redis.call('PEXPIRE', KEYS[1], ARGV[1]); ttl = tonumber(ARGV[1]) end
return {n, ttl}`)

// Allow counts one request for id under rule. A rule with Limit <= 0 is off.
func (l *Limiter) Allow(ctx context.Context, rule Rule, id string) (Result, error) {
	if l == nil || rule.Limit <= 0 || id == "" {
		return Result{Allowed: true}, nil
	}
	// Hash the ID: no emails, phone numbers or IPs stored in Redis.
	sum := sha256.Sum256([]byte(id))
	key := "rl:" + rule.Name + ":" + hex.EncodeToString(sum[:12])
	res, err := hit.Run(ctx, l.rdb, []string{key}, rule.Window.Milliseconds()).Int64Slice()
	if err == nil && len(res) != 2 {
		err = errors.New("unexpected reply from redis")
	}
	if err != nil {
		return Result{Allowed: true}, fmt.Errorf("rate limit %s: %w", rule.Name, err)
	}
	if res[0] <= int64(rule.Limit) {
		return Result{Allowed: true}, nil
	}
	retry := time.Duration(res[1]) * time.Millisecond
	if retry < time.Second {
		retry = time.Second
	}
	return Result{Allowed: false, RetryAfter: retry}, nil
}
