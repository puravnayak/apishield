package middleware

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/puravnayak/apishield/internal/policy"
	"github.com/puravnayak/apishield/internal/ratelimit"
)

type Middleware func(http.Handler) http.Handler

func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func Tracing() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			traceID := r.Header.Get("X-Trace-ID")
			if traceID == "" {
				traceID = uuid.New().String()
			}
			ctx := context.WithValue(r.Context(), TraceIDKey, traceID)
			w.Header().Set("X-Trace-ID", traceID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func Auth() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}

			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				http.Error(w, "Unauthorized: missing API Key", http.StatusUnauthorized)
				return
			}

			var tier string
			switch apiKey {
			case "ent-key":
				tier = "Enterprise"
			case "pro-key":
				tier = "Pro"
			default:
				tier = "Free"
			}

			ctx := context.WithValue(r.Context(), TierKey, tier)
			ctx = context.WithValue(ctx, APIKeyKey, apiKey)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RateLimit(cfg *policy.Config, limiter ratelimit.RateLimiter) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tier, _ := r.Context().Value(TierKey).(string)
			apiKey := r.Header.Get("X-API-Key")

			limit, ok := cfg.GetLimit(tier, r.URL.Path)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			key := fmt.Sprintf("rate_limit:%s:%s:%s", tier, r.URL.Path, apiKey)
			allowed, err := limiter.Allow(r.Context(), key, limit)
			if err != nil {
				http.Error(w, "Internal rate limiter error", http.StatusInternalServerError)
				return
			}

			if !allowed {
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func LoadShedder(currentLoad *int32, threshold int32) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			load := atomic.LoadInt32(currentLoad)
			if load >= threshold {
				tier, _ := r.Context().Value(TierKey).(string)
				if tier == "Free" || tier == "" {
					http.Error(w, "Service Unavailable (Load Shedding)", http.StatusServiceUnavailable)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
