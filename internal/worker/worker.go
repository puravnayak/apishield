package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/puravnayak/apishield/internal/circuitbreaker"
	"github.com/puravnayak/apishield/internal/events"
	"github.com/puravnayak/apishield/internal/metrics"
)

// WebhookWorker consumes webhook events from RabbitMQ and delivers them with resilience policies
type WebhookWorker struct {
	conn       *amqp.Connection
	eventStore events.EventStore
	cb         circuitbreaker.CircuitBreaker
	client     *http.Client
	queueName  string
	wg         sync.WaitGroup
}

// NewWebhookWorker initializes a WebhookWorker instance
func NewWebhookWorker(conn *amqp.Connection, eventStore events.EventStore, cb circuitbreaker.CircuitBreaker, queueName string) *WebhookWorker {
	return &WebhookWorker{
		conn:       conn,
		eventStore: eventStore,
		cb:         cb,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		queueName: queueName,
	}
}

// Start initiates the message consumer channel loop. Blocks until the context is cancelled.
func (w *WebhookWorker) Start(ctx context.Context) error {
	ch, err := w.conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to open channel: %w", err)
	}
	defer ch.Close()

	// QoS configuration sets the prefetch limit to 100 for concurrent goroutine handlers
	err = ch.Qos(100, 0, false)
	if err != nil {
		return fmt.Errorf("failed to set RabbitMQ QoS: %w", err)
	}

	msgs, err := ch.ConsumeWithContext(
		ctx,
		w.queueName,
		"",    // consumer tag
		false, // autoAck (false to manage manual ack/nack)
		false, // exclusive
		false, // noLocal
		false, // noWait
		nil,   // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to initiate consumer: %w", err)
	}

	log.Printf("WebhookWorker consumption started on queue %q", w.queueName)

	for {
		select {
		case <-ctx.Done():
			log.Println("Context closed, waiting for active delivery workers to finish...")
			w.wg.Wait()
			return nil
		case d, ok := <-msgs:
			if !ok {
				log.Println("RabbitMQ message channel closed, waiting for active handlers...")
				w.wg.Wait()
				return nil
			}

			w.wg.Add(1)
			go func(delivery amqp.Delivery) {
				defer w.wg.Done()
				w.handleDelivery(ctx, delivery)
			}(d)
		}
	}
}

func (w *WebhookWorker) handleDelivery(ctx context.Context, d amqp.Delivery) {
	var event events.WebhookEvent
	if err := json.Unmarshal(d.Body, &event); err != nil {
		log.Printf("Failed to unmarshal delivery body: %v", err)
		// Reject malformed payload permanently without requeuing (routes to DLQ)
		_ = d.Nack(false, false)
		return
	}

	// 1. Evaluate TargetURL against Circuit Breaker
	if !w.cb.Allow(event.TargetURL) {
		log.Printf("Circuit Breaker is OPEN for %s. Nacking event %s directly to DLQ.", event.TargetURL, event.ID)
		metrics.WebhookDeliveryAttempts.WithLabelValues("circuit_breaker_blocked").Inc()
		errMsg := fmt.Sprintf("delivery blocked: circuit breaker is in %s state", w.cb.State(event.TargetURL).String())
		if err := w.eventStore.UpdateEventStatus(ctx, event.ID, "failed", errMsg); err != nil {
			log.Printf("Error updating status for blocked event %s: %v", event.ID, err)
		}
		// Nack to send immediately to DLQ
		_ = d.Nack(false, false)
		return
	}

	// 2. Deliver event with Exponential Backoff
	err := w.deliverWithRetry(ctx, event)
	if err != nil {
		log.Printf("Failed to deliver event %s after retry attempts: %v", event.ID, err)
		metrics.WebhookDeliveryAttempts.WithLabelValues("failed").Inc()
		
		// Record Failure in Circuit Breaker state machine
		w.cb.RecordFailure(event.TargetURL)

		// Update DB state to failed
		if dbErr := w.eventStore.UpdateEventStatus(ctx, event.ID, "failed", err.Error()); dbErr != nil {
			log.Printf("Error updating failure status in DB for event %s: %v", event.ID, dbErr)
		}

		// Nack to DLQ
		_ = d.Nack(false, false)
		return
	}

	// 3. Successful delivery
	log.Printf("Successfully delivered event %s to %s", event.ID, event.TargetURL)
	metrics.WebhookDeliveryAttempts.WithLabelValues("success").Inc()

	// Record Success in Circuit Breaker state machine
	w.cb.RecordSuccess(event.TargetURL)

	// Update DB state to success
	if dbErr := w.eventStore.UpdateEventStatus(ctx, event.ID, "success", ""); dbErr != nil {
		log.Printf("Error updating success status in DB for event %s: %v", event.ID, dbErr)
	}

	// Ack message
	_ = d.Ack(false)
}

func (w *WebhookWorker) deliverWithRetry(ctx context.Context, event events.WebhookEvent) error {
	var lastErr error
	maxAttempts := 3

	for attempt := 0; attempt < maxAttempts; attempt++ {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)

		req, err := http.NewRequestWithContext(reqCtx, "POST", event.TargetURL, bytes.NewBuffer(event.Payload))
		if err != nil {
			cancel()
			return fmt.Errorf("failed to build http request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Trace-ID", event.ID)
		if event.IdempotencyKey != "" {
			req.Header.Set("X-Idempotency-Key", event.IdempotencyKey)
		}

		resp, err := w.client.Do(req)
		cancel()

		if err == nil {
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				resp.Body.Close()
				return nil
			}
			lastErr = fmt.Errorf("remote target responded with status code %d", resp.StatusCode)
			resp.Body.Close()
		} else {
			lastErr = fmt.Errorf("http post execution failed: %w", err)
		}

		// Sleep for backoff if not the final attempt
		if attempt < maxAttempts-1 {
			backoff := time.Duration(1<<attempt) * time.Second
			log.Printf("Webhook attempt %d failed for event %s. Retrying in %v. Last error: %v", attempt+1, event.ID, backoff, lastErr)

			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled during retry backoff: %w", ctx.Err())
			case <-time.After(backoff):
			}
		}
	}

	return lastErr
}
