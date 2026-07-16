package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/puravnayak/apishield/internal/cache"
	"github.com/puravnayak/apishield/internal/policy"
)

func TestStubRateLimiter(t *testing.T) {
	var limiter RateLimiter = NewStubRateLimiter()

	limit := policy.Limit{
		Algorithm: "sliding_window",
		Requests:  100,
		Window:    policy.Duration(time.Minute),
	}

	ctx := context.Background()
	allowed, err := limiter.Allow(ctx, "test-key", limit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected request to be allowed")
	}

	stats := limiter.Stats()
	if stats.AllowedRequests != 1 {
		t.Errorf("expected 1 allowed request, got %d", stats.AllowedRequests)
	}
}

func TestTwoTierRateLimiter(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skip("Redis is not running, skipping integrated rate limiter tests")
	}
	defer rdb.Close()

	key := "test-limit-key"
	rdb.Del(ctx, key)

	l1 := cache.NewShardedCache()
	limiter := NewTwoTierRateLimiter(rdb, l1)
	limiter.StartInvalidationListener(ctx)

	limit := policy.Limit{
		Algorithm: "sliding_window",
		Requests:  2,
		Window:    policy.Duration(200 * time.Millisecond),
	}

	allowed, err := limiter.Allow(ctx, key, limit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected first request to be allowed")
	}

	time.Sleep(30 * time.Millisecond) // Wait for L1 cache (TTL 20ms) to expire

	allowed, err = limiter.Allow(ctx, key, limit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected second request to be allowed")
	}

	time.Sleep(30 * time.Millisecond) // Wait for L1 cache (TTL 20ms) to expire

	allowed, err = limiter.Allow(ctx, key, limit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("expected third request to be blocked")
	}

	time.Sleep(250 * time.Millisecond)

	allowed, err = limiter.Allow(ctx, key, limit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected request to be allowed after window expires")
	}
}

func TestTwoTierRateLimiterInvalidation(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skip("Redis is not running, skipping integrated invalidation tests")
	}
	defer rdb.Close()

	key := "test-invalidation-key"
	rdb.Del(ctx, key)

	l1A := cache.NewShardedCache()
	limiterA := NewTwoTierRateLimiter(rdb, l1A)
	limiterA.StartInvalidationListener(ctx)

	l1B := cache.NewShardedCache()
	limiterB := NewTwoTierRateLimiter(rdb, l1B)
	limiterB.StartInvalidationListener(ctx)

	limit := policy.Limit{
		Algorithm: "sliding_window",
		Requests:  2,
		Window:    policy.Duration(time.Second),
	}

	allowed, err := limiterA.Allow(ctx, key, limit)
	if err != nil || !allowed {
		t.Fatalf("expected limiter A first request to be allowed: %v", err)
	}

	allowed, err = limiterB.Allow(ctx, key, limit)
	if err != nil || !allowed {
		t.Fatalf("expected limiter B first request to be allowed (hit L1 cache / bypass Redis if not blocked): %v", err)
	}

	time.Sleep(150 * time.Millisecond) // Wait for L1 cache of limiter A (TTL 100ms) to expire

	allowed, err = limiterA.Allow(ctx, key, limit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected limiter A second request to be blocked since L2 is at limit")
	}

	time.Sleep(100 * time.Millisecond) // Wait for invalidation message to propagate

	_, foundB := l1B.Get(key)
	if foundB {
		t.Error("expected key to be evicted from limiter B's L1 cache due to invalidation message")
	}
}
