package middleware

type contextKey string

const (
	TraceIDKey contextKey = "trace_id"
	TierKey    contextKey = "tier"
	APIKeyKey  contextKey = "api_key"
)
