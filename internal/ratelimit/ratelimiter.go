package ratelimit

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/puravnayak/apishield/internal/cache"
	"github.com/puravnayak/apishield/internal/policy"
)

type Stats struct {
	AllowedRequests  int64 `json:"allowed_requests"`
	RejectedRequests int64 `json:"rejected_requests"`
}

type RateLimiter interface {
	Allow(ctx context.Context, key string, limit policy.Limit) (bool, error)
	Stats() Stats
}

type TwoTierRateLimiter struct {
	l1            *cache.ShardedCache
	rdb           *redis.Client
	allowedCount  int64
	rejectedCount int64
}

func NewTwoTierRateLimiter(rdb *redis.Client, l1 *cache.ShardedCache) *TwoTierRateLimiter {
	return &TwoTierRateLimiter{
		l1:  l1,
		rdb: rdb,
	}
}

func (t *TwoTierRateLimiter) StartInvalidationListener(ctx context.Context) {
	pubsub := t.rdb.Subscribe(ctx, "apishield:invalidation")
	go func() {
		defer pubsub.Close()
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				t.l1.Delete(msg.Payload)
			}
		}
	}()
}

func (t *TwoTierRateLimiter) Allow(ctx context.Context, key string, limit policy.Limit) (bool, error) {
	if val, found := t.l1.Get(key); found {
		if allowed, ok := val.(bool); ok && allowed {
			atomic.AddInt64(&t.allowedCount, 1)
			return true, nil
		}
	}

	now := time.Now()
	nowMs := now.UnixNano() / int64(time.Millisecond)
	windowMs := limit.Window.Duration().Milliseconds()
	member := fmt.Sprintf("%d:%s", now.UnixNano(), uuid.New().String())

	res, err := t.rdb.Eval(ctx, slidingWindowLua, []string{key}, nowMs, windowMs, limit.Requests, member).Result()
	if err != nil {
		return false, fmt.Errorf("failed to execute rate limit lua script: %w", err)
	}

	allowedVal, ok := res.(int64)
	if !ok {
		return false, fmt.Errorf("unexpected lua script response: %v", res)
	}

	if allowedVal == 1 {
		l1TTL := limit.Window.Duration() / 10
		if l1TTL > time.Second {
			l1TTL = time.Second
		}
		t.l1.Set(key, true, l1TTL)
		atomic.AddInt64(&t.allowedCount, 1)
		return true, nil
	}

	_ = t.rdb.Publish(ctx, "apishield:invalidation", key).Err()
	atomic.AddInt64(&t.rejectedCount, 1)
	return false, nil
}

func (t *TwoTierRateLimiter) Stats() Stats {
	return Stats{
		AllowedRequests:  atomic.LoadInt64(&t.allowedCount),
		RejectedRequests: atomic.LoadInt64(&t.rejectedCount),
	}
}

type StubRateLimiter struct {
	allowedCount  int64
	rejectedCount int64
}

func NewStubRateLimiter() *StubRateLimiter {
	return &StubRateLimiter{}
}

func (s *StubRateLimiter) Allow(ctx context.Context, key string, limit policy.Limit) (bool, error) {
	atomic.AddInt64(&s.allowedCount, 1)
	return true, nil
}

func (s *StubRateLimiter) Stats() Stats {
	return Stats{
		AllowedRequests:  atomic.LoadInt64(&s.allowedCount),
		RejectedRequests: atomic.LoadInt64(&s.rejectedCount),
	}
}

