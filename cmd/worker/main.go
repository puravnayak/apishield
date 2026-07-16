package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/puravnayak/apishield/internal/cache"
	"github.com/puravnayak/apishield/internal/circuitbreaker"
	"github.com/puravnayak/apishield/internal/config"
	"github.com/puravnayak/apishield/internal/database"
	"github.com/puravnayak/apishield/internal/events"
	"github.com/puravnayak/apishield/internal/worker"
)

func main() {
	appCfg := config.Load()

	log.Println("Starting Webhook Worker Node...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pgPool, err := pgxpool.New(ctx, appCfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to initialize PostgreSQL pool: %v", err)
	}
	defer pgPool.Close()

	if err := database.InitSchema(ctx, pgPool); err != nil {
		log.Fatalf("Failed to initialize database schema: %v", err)
	}

	eventStore := events.NewPostgresEventStore(pgPool)

	rabbitmqConn, err := amqp.Dial(appCfg.RabbitMQURL)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer rabbitmqConn.Close()

	rdb, err := cache.NewRedisClient(appCfg.RedisAddr)
	if err != nil {
		log.Fatalf("Failed to initialize Redis client: %v", err)
	}
	defer rdb.Close()

	cb := circuitbreaker.NewURLCircuitBreaker(5, 30*time.Second)

	cb.OnStateChange(func(targetURL string, state circuitbreaker.State, failures int, lastFailure time.Time) {
		syncCtx := context.Background()
		var nextRetry time.Time
		if state == circuitbreaker.StateOpen {
			nextRetry = lastFailure.Add(30 * time.Second)
		}

		info := circuitbreaker.BreakerInfo{
			TargetURL:           targetURL,
			State:               state.String(),
			ConsecutiveFailures: failures,
			NextRetryAt:         nextRetry,
		}

		data, err := json.Marshal(info)
		if err != nil {
			log.Printf("Failed to marshal circuit breaker state: %v", err)
			return
		}

		if err := rdb.HSet(syncCtx, "apishield:circuit_breakers", targetURL, string(data)).Err(); err != nil {
			log.Printf("Failed to sync circuit breaker state to Redis: %v", err)
		} else {
			log.Printf("Synced Circuit Breaker state to Redis: %s -> %s", targetURL, state.String())
		}
	})

	http.HandleFunc("/v1/circuit-breakers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		breakers := cb.GetBreakers()
		_ = json.NewEncoder(w).Encode(breakers)
	})

	workerAddr := appCfg.WorkerAddr
	go func() {
		log.Printf("Starting Worker API on %s", workerAddr)
		if err := http.ListenAndServe(workerAddr, nil); err != nil {
			log.Printf("Worker API server error: %v", err)
		}
	}()

	w := worker.NewWebhookWorker(rabbitmqConn, eventStore, cb, "webhook_queue")

	go func() {
		if err := w.Start(ctx); err != nil {
			log.Fatalf("Worker failure: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down Webhook Worker gracefully...")
	cancel()

	time.Sleep(2 * time.Second)
	log.Println("Webhook Worker node stopped.")
}
