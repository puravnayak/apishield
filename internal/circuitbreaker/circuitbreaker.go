package circuitbreaker

import (
	"sync"
	"time"

	"github.com/puravnayak/apishield/internal/metrics"
)

// State represents the current state of a circuit breaker for a given endpoint
type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

// String returns the string representation of a State
func (s State) String() string {
	switch s {
	case StateClosed:
		return "Closed"
	case StateOpen:
		return "Open"
	case StateHalfOpen:
		return "Half-Open"
	default:
		return "Unknown"
	}
}

// CircuitBreaker defines the interface for the target URL state machine
type CircuitBreaker interface {
	Allow(url string) bool
	RecordSuccess(url string)
	RecordFailure(url string)
	State(url string) State
}

type urlState struct {
	mu          sync.Mutex
	state       State
	failures    int
	lastFailure time.Time
}

// URLCircuitBreaker tracks consecutive failures per TargetURL using a thread-safe map
type URLCircuitBreaker struct {
	mu            sync.RWMutex
	maxFailures   int
	openTimeout   time.Duration
	urlStates     map[string]*urlState
	onStateChange func(url string, state State, failures int, lastFailure time.Time)
}

// NewURLCircuitBreaker creates a new URLCircuitBreaker instance
func NewURLCircuitBreaker(maxFailures int, openTimeout time.Duration) *URLCircuitBreaker {
	return &URLCircuitBreaker{
		maxFailures: maxFailures,
		openTimeout: openTimeout,
		urlStates:   make(map[string]*urlState),
	}
}

func (cb *URLCircuitBreaker) getURLState(url string) *urlState {
	cb.mu.RLock()
	st, exists := cb.urlStates[url]
	cb.mu.RUnlock()

	if exists {
		return st
	}

	cb.mu.Lock()
	st, exists = cb.urlStates[url]
	if !exists {
		st = &urlState{state: StateClosed}
		cb.urlStates[url] = st
	}
	cb.mu.Unlock()
	return st
}

// OnStateChange sets the state change callback handler
func (cb *URLCircuitBreaker) OnStateChange(fn func(url string, state State, failures int, lastFailure time.Time)) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.onStateChange = fn
}

func (cb *URLCircuitBreaker) triggerChange(url string, state State, failures int, lastFailure time.Time) {
	cb.mu.RLock()
	fn := cb.onStateChange
	cb.mu.RUnlock()
	if fn != nil {
		fn(url, state, failures, lastFailure)
	}
}

// Allow determines if a request to the target URL should be allowed through
func (cb *URLCircuitBreaker) Allow(url string) bool {
	st := cb.getURLState(url)
	st.mu.Lock()
	oldState := st.state
	allowed := true
	if st.state == StateOpen {
		if time.Since(st.lastFailure) >= cb.openTimeout {
			st.state = StateHalfOpen
			allowed = true
		} else {
			allowed = false
		}
	}
	newState := st.state
	failures := st.failures
	lastFail := st.lastFailure
	st.mu.Unlock()

	if oldState != newState {
		cb.triggerChange(url, newState, failures, lastFail)
	}
	return allowed
}

// RecordSuccess marks a successful delivery to the target URL, resetting failures
func (cb *URLCircuitBreaker) RecordSuccess(url string) {
	st := cb.getURLState(url)
	st.mu.Lock()
	oldState := st.state
	if st.state == StateHalfOpen {
		st.state = StateClosed
	}
	st.failures = 0
	newState := st.state
	lastFail := st.lastFailure
	st.mu.Unlock()

	if oldState != newState {
		cb.triggerChange(url, newState, 0, lastFail)
	}
}

// RecordFailure increments consecutive failures, tripping to Open if threshold exceeded
func (cb *URLCircuitBreaker) RecordFailure(url string) {
	st := cb.getURLState(url)
	st.mu.Lock()
	oldState := st.state
	st.failures++
	st.lastFailure = time.Now()

	if st.state == StateHalfOpen || st.failures >= cb.maxFailures {
		if st.state != StateOpen {
			st.state = StateOpen
			metrics.CircuitBreakerTrips.WithLabelValues(url).Inc()
		}
	}
	newState := st.state
	failures := st.failures
	lastFail := st.lastFailure
	st.mu.Unlock()

	if oldState != newState || newState == StateOpen {
		cb.triggerChange(url, newState, failures, lastFail)
	}
}

// State returns the current State of the circuit breaker for the given URL
func (cb *URLCircuitBreaker) State(url string) State {
	st := cb.getURLState(url)
	st.mu.Lock()
	oldState := st.state
	if st.state == StateOpen && time.Since(st.lastFailure) >= cb.openTimeout {
		st.state = StateHalfOpen
	}
	newState := st.state
	failures := st.failures
	lastFail := st.lastFailure
	st.mu.Unlock()

	if oldState != newState {
		cb.triggerChange(url, newState, failures, lastFail)
	}
	return newState
}

// BreakerInfo holds information about a circuit breaker
type BreakerInfo struct {
	TargetURL           string    `json:"target_url"`
	State               string    `json:"state"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	NextRetryAt         time.Time `json:"next_retry_at"`
}

// GetBreakers returns all monitored URL circuit breakers
func (cb *URLCircuitBreaker) GetBreakers() []BreakerInfo {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	var infos []BreakerInfo
	for url, st := range cb.urlStates {
		st.mu.Lock()
		stateVal := st.state
		failures := st.failures
		lastFail := st.lastFailure
		st.mu.Unlock()

		actualState := stateVal
		var nextRetry time.Time
		if stateVal == StateOpen {
			nextRetry = lastFail.Add(cb.openTimeout)
			if time.Since(lastFail) > cb.openTimeout {
				actualState = StateHalfOpen
			}
		}

		infos = append(infos, BreakerInfo{
			TargetURL:           url,
			State:               actualState.String(),
			ConsecutiveFailures: failures,
			NextRetryAt:         nextRetry,
		})
	}
	return infos
}
