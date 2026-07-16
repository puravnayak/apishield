package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// GatewayRequestsTotal counts incoming HTTP requests handled by the gateway
	GatewayRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_requests_total",
			Help: "Total number of HTTP requests processed by the API Gateway",
		},
		[]string{"tier", "route", "status"},
	)

	// WebhookDeliveryAttempts counts webhook delivery tries by the worker pool
	WebhookDeliveryAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "webhook_delivery_attempts",
			Help: "Total number of webhook event delivery attempts made by the worker pool",
		},
		[]string{"status"}, // e.g. "success", "failed", "circuit_breaker_blocked"
	)

	// CircuitBreakerTrips counts instances where a target URL tripped to Open state
	CircuitBreakerTrips = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "circuit_breaker_trips",
			Help: "Total number of times a target URL tripped the circuit breaker to Open state",
		},
		[]string{"target_url"},
	)
)
