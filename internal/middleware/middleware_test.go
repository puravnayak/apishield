package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"github.com/puravnayak/apishield/internal/policy"
	"github.com/puravnayak/apishield/internal/ratelimit"
)

type dummyHandler struct{}

func (d *dummyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func TestTracing(t *testing.T) {
	h := Chain(&dummyHandler{}, Tracing())

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	traceID := rr.Result().Header.Get("X-Trace-ID")
	if traceID == "" {
		t.Error("expected trace ID in response headers")
	}
}

func TestAuth(t *testing.T) {
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tier, ok := r.Context().Value(TierKey).(string)
		if !ok {
			t.Fatal("expected Tier to be in context")
		}
		_, _ = w.Write([]byte(tier))
	}), Auth())

	// Test missing key
	reqMissing := httptest.NewRequest(http.MethodGet, "/test", nil)
	rrMissing := httptest.NewRecorder()
	h.ServeHTTP(rrMissing, reqMissing)
	if rrMissing.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rrMissing.Code)
	}

	// Test Enterprise key
	reqEnt := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqEnt.Header.Set("X-API-Key", "ent-key")
	rrEnt := httptest.NewRecorder()
	h.ServeHTTP(rrEnt, reqEnt)
	if rrEnt.Code != http.StatusOK || rrEnt.Body.String() != "Enterprise" {
		t.Errorf("expected OK and Enterprise, got %d and %s", rrEnt.Code, rrEnt.Body.String())
	}

	// Test Pro key
	reqPro := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqPro.Header.Set("X-API-Key", "pro-key")
	rrPro := httptest.NewRecorder()
	h.ServeHTTP(rrPro, reqPro)
	if rrPro.Code != http.StatusOK || rrPro.Body.String() != "Pro" {
		t.Errorf("expected OK and Pro, got %d and %s", rrPro.Code, rrPro.Body.String())
	}

	// Test default Free key
	reqFree := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqFree.Header.Set("X-API-Key", "other-key")
	rrFree := httptest.NewRecorder()
	h.ServeHTTP(rrFree, reqFree)
	if rrFree.Code != http.StatusOK || rrFree.Body.String() != "Free" {
		t.Errorf("expected OK and Free, got %d and %s", rrFree.Code, rrFree.Body.String())
	}
}

type staticMockRateLimiter struct {
	allowed bool
}

func (s *staticMockRateLimiter) Allow(ctx context.Context, key string, limit policy.Limit) (bool, error) {
	return s.allowed, nil
}

func (s *staticMockRateLimiter) Stats() ratelimit.Stats {
	return ratelimit.Stats{}
}

func TestRateLimitMiddleware(t *testing.T) {
	yamlInput := `
tiers:
  Pro:
    limits:
      - route: "/payments"
        limit:
          algorithm: "sliding_window"
          requests: 10
          window: "1m"
`
	cfg, err := policy.LoadConfigFromReader(strings.NewReader(yamlInput))
	if err != nil {
		t.Fatalf("failed to load policy config: %v", err)
	}

	mockLimiter := &staticMockRateLimiter{allowed: true}
	h := Chain(&dummyHandler{}, RateLimit(cfg, mockLimiter))

	// Test allowed request
	req := httptest.NewRequest(http.MethodPost, "/payments", nil)
	req = req.WithContext(context.WithValue(req.Context(), TierKey, "Pro"))
	req.Header.Set("X-API-Key", "pro-key")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	// Test blocked request
	mockLimiter.allowed = false
	reqBlocked := httptest.NewRequest(http.MethodPost, "/payments", nil)
	reqBlocked = reqBlocked.WithContext(context.WithValue(reqBlocked.Context(), TierKey, "Pro"))
	reqBlocked.Header.Set("X-API-Key", "pro-key")
	rrBlocked := httptest.NewRecorder()

	h.ServeHTTP(rrBlocked, reqBlocked)
	if rrBlocked.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rrBlocked.Code)
	}
}

func TestLoadShedderMiddleware(t *testing.T) {
	var currentLoad int32 = 50
	h := Chain(&dummyHandler{}, LoadShedder(&currentLoad, 80))

	// Under threshold
	reqFree := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqFree = reqFree.WithContext(context.WithValue(reqFree.Context(), TierKey, "Free"))
	rrFree := httptest.NewRecorder()
	h.ServeHTTP(rrFree, reqFree)
	if rrFree.Code != http.StatusOK {
		t.Errorf("expected 200 under threshold load, got %d", rrFree.Code)
	}

	// Exceeds threshold
	currentLoad = 90

	// Free tier blocked
	reqFreeBlocked := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqFreeBlocked = reqFreeBlocked.WithContext(context.WithValue(reqFreeBlocked.Context(), TierKey, "Free"))
	rrFreeBlocked := httptest.NewRecorder()
	h.ServeHTTP(rrFreeBlocked, reqFreeBlocked)
	if rrFreeBlocked.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for Free tier, got %d", rrFreeBlocked.Code)
	}

	// Pro tier allowed
	reqPro := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqPro = reqPro.WithContext(context.WithValue(reqPro.Context(), TierKey, "Pro"))
	rrPro := httptest.NewRecorder()
	h.ServeHTTP(rrPro, reqPro)
	if rrPro.Code != http.StatusOK {
		t.Errorf("expected 200 for Pro tier under load, got %d", rrPro.Code)
	}
}
