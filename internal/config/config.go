package config

import (
	"log"
	"os"
	"strings"
)

type Config struct {
	GatewayAddr      string
	DatabaseURL      string
	RedisAddr        string
	RabbitMQURL      string
	Environment      string
	PolicyConfigPath string
	ProxyTargetURL   string
	PprofAddr        string
	WorkerAddr       string
	WorkerAPIURL     string
}

func Load() *Config {
	LoadEnv()

	cfg := &Config{
		GatewayAddr:      GetEnv("GATEWAY_ADDR", ":8080"),
		DatabaseURL:      GetEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/apishield?sslmode=disable"),
		RedisAddr:        GetEnv("REDIS_ADDR", "localhost:6379"),
		RabbitMQURL:      GetEnv("RABBITMQ_URL", "amqp://shield_admin:shield_secret@localhost:5672/"),
		Environment:      GetEnv("ENVIRONMENT", "development"),
		PolicyConfigPath: GetEnv("POLICY_CONFIG_PATH", "config.yaml"),
		ProxyTargetURL:   GetEnv("PROXY_TARGET_URL", "https://httpbin.org"),
		PprofAddr:        GetEnv("PPROF_ADDR", ":6060"),
		WorkerAddr:       GetEnv("WORKER_ADDR", ":8081"),
		WorkerAPIURL:     GetEnv("WORKER_API_URL", "http://localhost:8081"),
	}

	if strings.ToLower(cfg.Environment) == "production" {
		if os.Getenv("DATABASE_URL") == "" {
			log.Fatalf("DATABASE_URL missing in production")
		}
		if os.Getenv("RABBITMQ_URL") == "" {
			log.Fatalf("RABBITMQ_URL missing in production")
		}
	}

	return cfg
}
