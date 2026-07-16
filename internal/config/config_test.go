package config

import (
	"os"
	"testing"
)

func TestConfigLoadDefaults(t *testing.T) {
	// Temporarily clear environment variables to ensure fallbacks work
	origGatewayAddr := os.Getenv("GATEWAY_ADDR")
	origDatabaseURL := os.Getenv("DATABASE_URL")
	origRedisAddr := os.Getenv("REDIS_ADDR")
	origRabbitMQURL := os.Getenv("RABBITMQ_URL")
	origEnvironment := os.Getenv("ENVIRONMENT")

	os.Unsetenv("GATEWAY_ADDR")
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("REDIS_ADDR")
	os.Unsetenv("RABBITMQ_URL")
	os.Unsetenv("ENVIRONMENT")

	defer func() {
		os.Setenv("GATEWAY_ADDR", origGatewayAddr)
		os.Setenv("DATABASE_URL", origDatabaseURL)
		os.Setenv("REDIS_ADDR", origRedisAddr)
		os.Setenv("RABBITMQ_URL", origRabbitMQURL)
		os.Setenv("ENVIRONMENT", origEnvironment)
	}()

	cfg := Load()
	if cfg.GatewayAddr != ":8080" {
		t.Errorf("expected default GatewayAddr :8080, got %q", cfg.GatewayAddr)
	}
	if cfg.Environment != "development" {
		t.Errorf("expected default Environment development, got %q", cfg.Environment)
	}
}

func TestConfigLoadFromEnv(t *testing.T) {
	origGatewayAddr := os.Getenv("GATEWAY_ADDR")
	origEnvironment := os.Getenv("ENVIRONMENT")

	os.Setenv("GATEWAY_ADDR", ":9090")
	os.Setenv("ENVIRONMENT", "staging")

	defer func() {
		os.Setenv("GATEWAY_ADDR", origGatewayAddr)
		os.Setenv("ENVIRONMENT", origEnvironment)
	}()

	cfg := Load()
	if cfg.GatewayAddr != ":9090" {
		t.Errorf("expected overridden GatewayAddr :9090, got %q", cfg.GatewayAddr)
	}
	if cfg.Environment != "staging" {
		t.Errorf("expected overridden Environment staging, got %q", cfg.Environment)
	}
}
