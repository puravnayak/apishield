package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// WebhookEvent represents a webhook delivery task stored in PostgreSQL and routed through RabbitMQ
type WebhookEvent struct {
	ID             string          `json:"id"`
	APIKey         string          `json:"api_key"`
	TargetURL      string          `json:"target_url"`
	Payload        json.RawMessage `json:"payload"`
	Status         string          `json:"status"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	RetryCount     int             `json:"retry_count"`
	ErrorMessage   *string         `json:"error_message,omitempty"`
	CreatedAt      string          `json:"created_at,omitempty"`
}

// EventStore defines the interface for persisting webhook events
type EventStore interface {
	SaveEvent(ctx context.Context, event WebhookEvent) error
	UpdateEventStatus(ctx context.Context, id string, status string, errMsg string) error
	GetFailedEvents(ctx context.Context, apiKey string) ([]WebhookEvent, error)
	GetEvents(ctx context.Context, limit int, status string) ([]WebhookEvent, error)
}

// PostgresEventStore implements the EventStore interface using pgxpool for PostgreSQL
type PostgresEventStore struct {
	pool *pgxpool.Pool
}

// NewPostgresEventStore creates a new PostgresEventStore instance
func NewPostgresEventStore(pool *pgxpool.Pool) *PostgresEventStore {
	return &PostgresEventStore{pool: pool}
}

// SaveEvent inserts a new webhook event record into the PostgreSQL database.
// If an event with the same idempotency key already exists, the insert is skipped (ON CONFLICT DO NOTHING).
func (s *PostgresEventStore) SaveEvent(ctx context.Context, event WebhookEvent) error {
	idempKey := event.IdempotencyKey
	if idempKey == "" {
		idempKey = event.ID
	}

	query := `
		INSERT INTO events (id, idempotency_key, api_key, target_url, payload, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (idempotency_key) DO NOTHING
	`

	_, err := s.pool.Exec(ctx, query, event.ID, idempKey, event.APIKey, event.TargetURL, event.Payload, event.Status)
	if err != nil {
		return fmt.Errorf("failed to execute insert event query: %w", err)
	}

	return nil
}

// UpdateEventStatus updates the status and any error message of an existing webhook event.
func (s *PostgresEventStore) UpdateEventStatus(ctx context.Context, id string, status string, errMsg string) error {
	query := `
		UPDATE events
		SET status = $1, error_message = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3
	`

	var errPtr *string
	if errMsg != "" {
		errPtr = &errMsg
	}

	_, err := s.pool.Exec(ctx, query, status, errPtr, id)
	if err != nil {
		return fmt.Errorf("failed to execute update event status query: %w", err)
	}

	return nil
}

// GetFailedEvents retrieves all failed webhook events associated with the provided api_key
func (s *PostgresEventStore) GetFailedEvents(ctx context.Context, apiKey string) ([]WebhookEvent, error) {
	query := `
		SELECT id, api_key, target_url, payload, status, idempotency_key
		FROM events
		WHERE api_key = $1 AND status = 'failed'
	`

	rows, err := s.pool.Query(ctx, query, apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to query failed events: %w", err)
	}
	defer rows.Close()

	var eventsList []WebhookEvent
	for rows.Next() {
		var ev WebhookEvent
		err := rows.Scan(&ev.ID, &ev.APIKey, &ev.TargetURL, &ev.Payload, &ev.Status, &ev.IdempotencyKey)
		if err != nil {
			return nil, fmt.Errorf("failed to scan failed event row: %w", err)
		}
		eventsList = append(eventsList, ev)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error for failed events query: %w", err)
	}

	return eventsList, nil
}

// GetEvents retrieves a list of events from the database up to a limit, optionally filtered by status
func (s *PostgresEventStore) GetEvents(ctx context.Context, limit int, status string) ([]WebhookEvent, error) {
	var query string
	var args []interface{}

	if status != "" {
		query = `
			SELECT id, api_key, target_url, payload, status, idempotency_key, retry_count, error_message, to_char(created_at, 'YYYY-MM-DD HH24:MI:SS')
			FROM events
			WHERE status = $1
			ORDER BY created_at DESC
			LIMIT $2
		`
		args = []interface{}{status, limit}
	} else {
		query = `
			SELECT id, api_key, target_url, payload, status, idempotency_key, retry_count, error_message, to_char(created_at, 'YYYY-MM-DD HH24:MI:SS')
			FROM events
			ORDER BY created_at DESC
			LIMIT $1
		`
		args = []interface{}{limit}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	var eventsList []WebhookEvent
	for rows.Next() {
		var ev WebhookEvent
		err := rows.Scan(
			&ev.ID,
			&ev.APIKey,
			&ev.TargetURL,
			&ev.Payload,
			&ev.Status,
			&ev.IdempotencyKey,
			&ev.RetryCount,
			&ev.ErrorMessage,
			&ev.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event row: %w", err)
		}
		eventsList = append(eventsList, ev)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error for events query: %w", err)
	}

	return eventsList, nil
}
