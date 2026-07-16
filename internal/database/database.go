package database

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

func InitSchema(ctx context.Context, pool *pgxpool.Pool) error {
	log.Println("Initializing database schema...")

	_, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`)
	if err != nil {
		return fmt.Errorf("failed to create uuid-ossp extension: %w", err)
	}

	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS events (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			idempotency_key VARCHAR(255) UNIQUE,
			api_key VARCHAR(255) NOT NULL,
			target_url TEXT NOT NULL,
			payload JSONB NOT NULL,
			status VARCHAR(50) NOT NULL DEFAULT 'pending',
			retry_count INT NOT NULL DEFAULT 0,
			next_retry_at TIMESTAMP WITH TIME ZONE,
			error_message TEXT,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create events table: %w", err)
	}

	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS api_keys (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			key_hash VARCHAR(64) UNIQUE NOT NULL,
			client_name VARCHAR(255) NOT NULL,
			tier VARCHAR(50) NOT NULL,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create api_keys table: %w", err)
	}

	_, err = pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_events_status ON events(status);`)
	if err != nil {
		return fmt.Errorf("failed to create idx_events_status index: %w", err)
	}

	_, err = pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at);`)
	if err != nil {
		return fmt.Errorf("failed to create idx_events_created_at index: %w", err)
	}

	_, err = pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_api_keys_is_active ON api_keys(is_active);`)
	if err != nil {
		return fmt.Errorf("failed to create idx_api_keys_is_active index: %w", err)
	}

	log.Println("Database schema verified.")
	return nil
}
