package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestAllowWindow(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	l := New(rdb, DefaultRules())
	rule := Rule{"test", 3, time.Minute}

	for i := range 3 {
		if res, err := l.Allow(ctx, rule, "1.2.3.4"); err != nil || !res.Allowed {
			t.Fatalf("request %d refused: %+v %v", i+1, res, err)
		}
	}
	res, err := l.Allow(ctx, rule, "1.2.3.4")
	if err != nil || res.Allowed || res.RetryAfter <= 0 || res.RetryAfter > time.Minute {
		t.Fatalf("4th request: %+v %v", res, err)
	}
	// Keys are independent.
	if res, _ := l.Allow(ctx, rule, "5.6.7.8"); !res.Allowed {
		t.Error("other key refused")
	}
	// The raw ID is not stored in Redis.
	for _, k := range mr.Keys() {
		if k == "rl:test:1.2.3.4" {
			t.Errorf("unhashed key %s", k)
		}
	}
	// A new window starts after the old one expires.
	mr.FastForward(time.Minute + time.Second)
	if res, _ := l.Allow(ctx, rule, "1.2.3.4"); !res.Allowed {
		t.Error("refused after the window")
	}
}

func TestAllowDisabledAndRedisDown(t *testing.T) {
	ctx := context.Background()
	var nilLimiter *Limiter
	if res, err := nilLimiter.Allow(ctx, Rule{"x", 1, time.Minute}, "k"); !res.Allowed || err != nil {
		t.Error("nil limiter must allow")
	}

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	l := New(rdb, DefaultRules())
	if res, _ := l.Allow(ctx, Rule{"off", 0, time.Minute}, "k"); !res.Allowed {
		t.Error("limit 0 means off")
	}
	mr.Close()
	res, err := l.Allow(ctx, Rule{"x", 1, time.Minute}, "k")
	if !res.Allowed || err == nil {
		t.Errorf("redis down: must allow and report the error, got %+v %v", res, err)
	}
}
