package circuitbreaker

import (
	"testing"
	"time"
)

func TestURLCircuitBreaker_Transitions(t *testing.T) {
	cb := NewURLCircuitBreaker(3, 50*time.Millisecond)
	url := "http://test-endpoint.com"

	// 1. Initial state should be Closed, Allow = true
	if state := cb.State(url); state != StateClosed {
		t.Errorf("expected initial state Closed, got %v", state)
	}
	if !cb.Allow(url) {
		t.Error("expected Allow to be true in Closed state")
	}

	// 2. Record 2 failures (limit is 3) -> should remain Closed
	cb.RecordFailure(url)
	cb.RecordFailure(url)
	if state := cb.State(url); state != StateClosed {
		t.Errorf("expected state Closed after 2 failures, got %v", state)
	}
	if !cb.Allow(url) {
		t.Error("expected Allow to be true in Closed state after 2 failures")
	}

	// 3. Record 3rd failure -> should transition to Open, Allow = false
	cb.RecordFailure(url)
	if state := cb.State(url); state != StateOpen {
		t.Errorf("expected state Open after 3 failures, got %v", state)
	}
	if cb.Allow(url) {
		t.Error("expected Allow to be false in Open state")
	}

	// 4. Wait for openTimeout (50ms) -> should transition to Half-Open
	time.Sleep(60 * time.Millisecond)
	if state := cb.State(url); state != StateHalfOpen {
		t.Errorf("expected state Half-Open after timeout, got %v", state)
	}
	if !cb.Allow(url) {
		t.Error("expected Allow to be true in Half-Open state")
	}

	// 5. In Half-Open, record success -> should transition to Closed
	cb.RecordSuccess(url)
	if state := cb.State(url); state != StateClosed {
		t.Errorf("expected state Closed after success in Half-Open, got %v", state)
	}
	if !cb.Allow(url) {
		t.Error("expected Allow to be true in Closed state")
	}

	// 6. Record 3 failures again to trip it
	cb.RecordFailure(url)
	cb.RecordFailure(url)
	cb.RecordFailure(url)
	if state := cb.State(url); state != StateOpen {
		t.Errorf("expected state Open after 3 failures, got %v", state)
	}

	// 7. Wait for timeout again -> Half-Open
	time.Sleep(60 * time.Millisecond)
	if state := cb.State(url); state != StateHalfOpen {
		t.Errorf("expected state Half-Open after timeout, got %v", state)
	}

	// 8. In Half-Open, record failure -> should transition immediately back to Open
	cb.RecordFailure(url)
	if state := cb.State(url); state != StateOpen {
		t.Errorf("expected state Open immediately after failure in Half-Open, got %v", state)
	}
	if cb.Allow(url) {
		t.Error("expected Allow to be false after failure in Half-Open")
	}
}
