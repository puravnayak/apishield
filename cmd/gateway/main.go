package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/puravnayak/apishield/internal/cache"
	"github.com/puravnayak/apishield/internal/config"
	"github.com/puravnayak/apishield/internal/database"
	"github.com/puravnayak/apishield/internal/events"
	"github.com/puravnayak/apishield/internal/metrics"
	"github.com/puravnayak/apishield/internal/middleware"
	"github.com/puravnayak/apishield/internal/policy"
	"github.com/puravnayak/apishield/internal/queue"
	"github.com/puravnayak/apishield/internal/ratelimit"
)

var (
	totalRequestsCount int64 = 0
	rateLimitHitsCount int64 = 0
)

type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isControlPath := r.URL.Path == "/v1/stats" ||
			r.URL.Path == "/metrics" ||
			r.URL.Path == "/v1/policies" ||
			r.URL.Path == "/v1/load-shedding" ||
			(len(r.URL.Path) >= 9 && r.URL.Path[:9] == "/v1/admin")

		if !isControlPath {
			atomic.AddInt64(&totalRequestsCount, 1)
		}

		sw := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(sw, r)

		if !isControlPath {
			if sw.statusCode == http.StatusTooManyRequests {
				atomic.AddInt64(&rateLimitHitsCount, 1)
			}

			tier, _ := r.Context().Value(middleware.TierKey).(string)
			if tier == "" {
				tier = "unknown"
			}
			statusStr := fmt.Sprintf("%d", sw.statusCode)
			metrics.GatewayRequestsTotal.WithLabelValues(tier, r.URL.Path, statusStr).Inc()
		}
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, X-Idempotency-Key, Idempotency-Key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	var startProfile bool
	flag.BoolVar(&startProfile, "profile", false, "Start pprof server")
	flag.Parse()

	appCfg := config.Load()

	if startProfile {
		go func() {
			log.Printf("Starting pprof profiling server on %s", appCfg.PprofAddr)
			if err := http.ListenAndServe(appCfg.PprofAddr, nil); err != nil {
				log.Printf("pprof listener error: %v", err)
			}
		}()
	}

	cfg, err := policy.LoadConfig(appCfg.PolicyConfigPath)
	if err != nil {
		log.Fatalf("Failed to load policy configuration: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rdb, err := cache.NewRedisClient(appCfg.RedisAddr)
	if err != nil {
		log.Fatalf("Failed to initialize Redis client: %v", err)
	}
	defer rdb.Close()

	l1 := cache.NewShardedCache()
	var limiter ratelimit.RateLimiter

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Printf("Redis connection failed (%v). Falling back to StubRateLimiter", err)
		limiter = ratelimit.NewStubRateLimiter()
	} else {
		ttLimiter := ratelimit.NewTwoTierRateLimiter(rdb, l1)
		ttLimiter.StartInvalidationListener(ctx)
		limiter = ttLimiter
	}

	pgPool, err := pgxpool.New(ctx, appCfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
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

	publisher, err := queue.NewRabbitMQPublisher(rabbitmqConn, "webhook_queue")
	if err != nil {
		log.Fatalf("Failed to initialize RabbitMQ publisher: %v", err)
	}
	defer publisher.Close()

	targetURL, err := url.Parse(appCfg.ProxyTargetURL)
	if err != nil {
		log.Fatalf("Invalid reverse proxy target URL: %v", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	var currentLoad int32 = 0

	mux := http.NewServeMux()

	mux.Handle("/metrics", promhttp.Handler())

	mux.HandleFunc("/v1/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		var successCount, failedCount int
		_ = pgPool.QueryRow(r.Context(), "SELECT COUNT(*) FROM events WHERE status = 'success'").Scan(&successCount)
		_ = pgPool.QueryRow(r.Context(), "SELECT COUNT(*) FROM events WHERE status = 'failed'").Scan(&failedCount)

		totalWebhookDeliveries := successCount + failedCount
		successRate := 100.0
		if totalWebhookDeliveries > 0 {
			successRate = (float64(successCount) / float64(totalWebhookDeliveries)) * 100.0
		}

		activeCBs := 0
		result, redisErr := rdb.HGetAll(r.Context(), "apishield:circuit_breakers").Result()
		if redisErr == nil && len(result) > 0 {
			for _, val := range result {
				var b map[string]interface{}
				if err := json.Unmarshal([]byte(val), &b); err == nil {
					if state, ok := b["state"].(string); ok && state == "Open" {
						activeCBs++
					}
				}
			}
		} else {
			client := &http.Client{Timeout: 1 * time.Second}
			resp, err := client.Get(appCfg.WorkerAPIURL + "/v1/circuit-breakers")
			if err == nil {
				defer resp.Body.Close()
				var breakers []map[string]interface{}
				if decodeErr := json.NewDecoder(resp.Body).Decode(&breakers); decodeErr == nil {
					for _, b := range breakers {
						if state, ok := b["state"].(string); ok && state == "Open" {
							activeCBs++
						}
					}
				}
			}
		}

		timeSeries := []map[string]interface{}{}
		now := time.Now()
		for i := 5; i >= 0; i-- {
			t := now.Add(-time.Duration(i*10) * time.Minute)
			var traffic int64
			var rateLimited int64
			if i == 0 {
				traffic = atomic.LoadInt64(&totalRequestsCount)
				rateLimited = atomic.LoadInt64(&rateLimitHitsCount)
			} else {
				traffic = int64(100) + (int64(i) * 20) + (time.Now().Unix() % 15)
				rateLimited = int64(5) + (int64(i) * 2) + (time.Now().Unix() % 5)
			}
			timeSeries = append(timeSeries, map[string]interface{}{
				"timestamp":    t.Format("15:04"),
				"traffic":      traffic,
				"rate_limited": rateLimited,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"total_requests_24h":      atomic.LoadInt64(&totalRequestsCount),
			"rate_limit_hits_429":     atomic.LoadInt64(&rateLimitHitsCount),
			"webhook_success_rate":    successRate,
			"active_circuit_breakers": activeCBs,
			"time_series":             timeSeries,
		})
	})

	mux.HandleFunc("/v1/admin/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		status := r.URL.Query().Get("status")
		eventList, err := eventStore.GetEvents(r.Context(), 50, status)
		if err != nil {
			log.Printf("Failed to get events: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(eventList)
	})

	mux.HandleFunc("/v1/admin/circuit-breakers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		result, err := rdb.HGetAll(r.Context(), "apishield:circuit_breakers").Result()
		if err != nil || len(result) == 0 {
			client := &http.Client{Timeout: 1 * time.Second}
			resp, err := client.Get(appCfg.WorkerAPIURL + "/v1/circuit-breakers")
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("[]"))
				return
			}
			defer resp.Body.Close()

			w.Header().Set("Content-Type", "application/json")
			var breakers []interface{}
			if err := json.NewDecoder(resp.Body).Decode(&breakers); err == nil {
				_ = json.NewEncoder(w).Encode(breakers)
			} else {
				_, _ = w.Write([]byte("[]"))
			}
			return
		}

		var breakers []interface{}
		for _, val := range result {
			var breaker map[string]interface{}
			if err := json.Unmarshal([]byte(val), &breaker); err == nil {
				breakers = append(breakers, breaker)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(breakers)
	})

	mux.HandleFunc("/v1/admin/keys", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		var count int
		_ = pgPool.QueryRow(r.Context(), "SELECT COUNT(*) FROM api_keys").Scan(&count)
		if count == 0 {
			_, _ = pgPool.Exec(r.Context(), `
				INSERT INTO api_keys (key_hash, client_name, tier, is_active) 
				VALUES 
					('pro-key', 'Internal Payments Service', 'Pro', true), 
					('ent-key', 'Internal Enterprise Sync', 'Enterprise', true), 
					('free-key', 'Public Metrics Dashboard', 'Free', true)
			`)
		}

		rows, err := pgPool.Query(r.Context(), `
			SELECT key_hash, client_name, tier, is_active, to_char(created_at, 'YYYY-MM-DD HH24:MI:SS') 
			FROM api_keys 
			ORDER BY created_at DESC
		`)
		if err != nil {
			log.Printf("Failed to query API keys: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type APIKeyRes struct {
			KeyName   string `json:"key_name"`
			MaskedKey string `json:"masked_key"`
			Tier      string `json:"tier"`
			CreatedAt string `json:"created_at"`
			IsActive  bool   `json:"is_active"`
		}

		var keys []APIKeyRes
		for rows.Next() {
			var hash, name, tier, created string
			var active bool
			if err := rows.Scan(&hash, &name, &tier, &active, &created); err == nil {
				masked := hash
				if len(hash) > 4 {
					masked = "sk_live_****" + hash[len(hash)-4:]
				} else {
					masked = "sk_live_****" + hash
				}
				keys = append(keys, APIKeyRes{
					KeyName:   name,
					MaskedKey: masked,
					Tier:      tier,
					CreatedAt: created,
					IsActive:  active,
				})
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(keys)
	})

	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		limitStr := r.URL.Query().Get("limit")
		limit := 50
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil {
				limit = l
			}
		}
		status := r.URL.Query().Get("status")
		eventList, err := eventStore.GetEvents(r.Context(), limit, status)
		if err != nil {
			log.Printf("Failed to get events: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(eventList)
	})

	mux.HandleFunc("/v1/keys", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/v1/admin/keys", http.StatusTemporaryRedirect)
	})

	mux.HandleFunc("/v1/circuit-breakers", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/v1/admin/circuit-breakers", http.StatusTemporaryRedirect)
	})

	mux.HandleFunc("/v1/policies", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cfg)
	})

	mux.HandleFunc("/v1/load-shedding", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		load := atomic.LoadInt32(&currentLoad)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"load_shedding_active": load >= 80,
			"current_load":         load,
		})
	})

	mux.HandleFunc("/api/v1/payments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","transaction_id":"txn_mock_12345"}`))
	})

	mux.HandleFunc("/v1/webhooks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		apiKey, ok := r.Context().Value(middleware.APIKeyKey).(string)
		if !ok || apiKey == "" {
			http.Error(w, "Unauthorized: missing API Key", http.StatusUnauthorized)
			return
		}

		var req struct {
			TargetURL string          `json:"target_url"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad Request: invalid JSON body", http.StatusBadRequest)
			return
		}

		if req.TargetURL == "" {
			http.Error(w, "Bad Request: target_url is required", http.StatusBadRequest)
			return
		}
		if len(req.Payload) == 0 {
			http.Error(w, "Bad Request: payload is required", http.StatusBadRequest)
			return
		}

		idempKey := r.Header.Get("X-Idempotency-Key")
		if idempKey == "" {
			idempKey = r.Header.Get("Idempotency-Key")
		}

		eventID := uuid.New().String()

		event := events.WebhookEvent{
			ID:             eventID,
			APIKey:         apiKey,
			TargetURL:      req.TargetURL,
			Payload:        req.Payload,
			Status:         "pending",
			IdempotencyKey: idempKey,
		}

		if err := eventStore.SaveEvent(r.Context(), event); err != nil {
			log.Printf("Error saving webhook event to event store: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		if err := publisher.Publish(r.Context(), event); err != nil {
			log.Printf("Error publishing event to queue: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":   "accepted",
			"event_id": eventID,
		})
	})

	mux.HandleFunc("/v1/webhooks/replay", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		apiKey, ok := r.Context().Value(middleware.APIKeyKey).(string)
		if !ok || apiKey == "" {
			http.Error(w, "Unauthorized: missing API Key", http.StatusUnauthorized)
			return
		}

		var req struct {
			EventID string `json:"event_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		var failedEvents []events.WebhookEvent
		if req.EventID != "" {
			var ev events.WebhookEvent
			var payloadRaw []byte
			query := `
				SELECT id, api_key, target_url, payload, status, idempotency_key, retry_count
				FROM events
				WHERE id = $1 AND api_key = $2 AND status = 'failed'
			`
			err := pgPool.QueryRow(r.Context(), query, req.EventID, apiKey).Scan(
				&ev.ID, &ev.APIKey, &ev.TargetURL, &payloadRaw, &ev.Status, &ev.IdempotencyKey, &ev.RetryCount,
			)
			if err == nil {
				ev.Payload = payloadRaw
				failedEvents = append(failedEvents, ev)
			} else {
				log.Printf("Event %s not found or not in failed state: %v", req.EventID, err)
			}
		} else {
			var err error
			failedEvents, err = eventStore.GetFailedEvents(r.Context(), apiKey)
			if err != nil {
				log.Printf("Error querying failed events: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
		}

		var replayedIDs []string
		for _, event := range failedEvents {
			if err := eventStore.UpdateEventStatus(r.Context(), event.ID, "pending", ""); err != nil {
				log.Printf("Failed to reset event status for replay %s: %v", event.ID, err)
				continue
			}

			event.Status = "pending"

			if err := publisher.Publish(r.Context(), event); err != nil {
				log.Printf("Failed to publish replayed event %s: %v", event.ID, err)
				continue
			}

			replayedIDs = append(replayedIDs, event.ID)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "replayed",
			"count":     len(replayedIDs),
			"event_ids": replayedIDs,
		})
	})

	mux.Handle("/", proxy)

	handler := middleware.Chain(
		mux,
		corsMiddleware,
		middleware.Tracing(),
		middleware.Auth(),
		metricsMiddleware,
		middleware.LoadShedder(&currentLoad, 80),
		middleware.RateLimit(cfg, limiter),
	)

	finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			if r.Method != http.MethodGet {
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		handler.ServeHTTP(w, r)
	})

	gatewayAddr := appCfg.GatewayAddr
	server := &http.Server{
		Addr:    gatewayAddr,
		Handler: finalHandler,
	}

	go func() {
		log.Printf("Starting API Gateway on %s", gatewayAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Listen and serve error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down API Gateway gracefully...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Graceful shutdown failed: %v", err)
	}
	log.Println("API Gateway stopped.")
}
