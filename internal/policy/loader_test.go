package policy

import (
	"strings"
	"testing"
	"time"
)

func TestLoadConfigFromReader(t *testing.T) {
	yamlInput := `
tiers:
  Enterprise:
    limits:
      - route: "/api/v1/payments"
        limit:
          algorithm: "sliding_window"
          requests: 1000
          window: "1m"
      - route: "/*"
        limit:
          algorithm: "sliding_window"
          requests: 5000
          window: "10s"
`
	cfg, err := LoadConfigFromReader(strings.NewReader(yamlInput))
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	enterpriseRules, ok := cfg.Tiers["Enterprise"]
	if !ok {
		t.Fatal("expected Enterprise tier to exist")
	}

	if len(enterpriseRules.Limits) != 2 {
		t.Fatalf("expected 2 limits, got %d", len(enterpriseRules.Limits))
	}

	rule1 := enterpriseRules.Limits[0]
	if rule1.Route != "/api/v1/payments" {
		t.Errorf("expected route /api/v1/payments, got %s", rule1.Route)
	}
	if rule1.Limit.Algorithm != "sliding_window" {
		t.Errorf("expected algorithm sliding_window, got %s", rule1.Limit.Algorithm)
	}
	if rule1.Limit.Requests != 1000 {
		t.Errorf("expected 1000 requests, got %d", rule1.Limit.Requests)
	}
	if rule1.Limit.Window.Duration() != time.Minute {
		t.Errorf("expected window 1m, got %v", rule1.Limit.Window.Duration())
	}

	rule2 := enterpriseRules.Limits[1]
	if rule2.Limit.Window.Duration() != 10*time.Second {
		t.Errorf("expected window 10s, got %v", rule2.Limit.Window.Duration())
	}
}

func TestGetLimit(t *testing.T) {
	yamlInput := `
tiers:
  Pro:
    limits:
      - route: "/api/v1/payments"
        limit:
          algorithm: "sliding_window"
          requests: 100
          window: "1m"
      - route: "/*"
        limit:
          algorithm: "sliding_window"
          requests: 500
          window: "1m"
`
	cfg, err := LoadConfigFromReader(strings.NewReader(yamlInput))
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	limit, ok := cfg.GetLimit("Pro", "/api/v1/payments")
	if !ok {
		t.Error("expected limit to be found")
	}
	if limit.Requests != 100 {
		t.Errorf("expected 100 requests, got %d", limit.Requests)
	}

	limit, ok = cfg.GetLimit("Pro", "/api/v1/users")
	if !ok {
		t.Error("expected fallback limit to be found")
	}
	if limit.Requests != 500 {
		t.Errorf("expected 500 requests, got %d", limit.Requests)
	}

	_, ok = cfg.GetLimit("Free", "/api/v1/payments")
	if ok {
		t.Error("expected no limit to be found for non-existing tier")
	}
}
