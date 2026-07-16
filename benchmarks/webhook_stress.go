package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	targetURL := "http://localhost:8080/v1/webhooks"
	apiKey := "ent-key"
	concurrency := 40
	duration := 15 * time.Second

	fmt.Printf("Starting APIShield Webhook Stress Test...\n")
	fmt.Printf("Concurrency: %d concurrent workers\n", concurrency)
	fmt.Printf("Duration: %s\n", duration)
	fmt.Printf("Target: %s\n\n", targetURL)

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var sentCount int64
	var successCount int64
	var rateLimitCount int64
	var errorCount int64

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 200,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	start := time.Now()
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			// Send requests periodically with a slight offset
			ticker := time.NewTicker(40 * time.Millisecond) // send ~25 requests/sec per worker (total ~1000 RPS)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					// Alternate targets: half succeeds, half fails downstream
					deliveryTarget := "http://localhost:8080/api/v1/payments"
					if (workerID + int(atomic.LoadInt64(&sentCount)))%2 == 0 {
						deliveryTarget = "http://localhost:9999/fail"
					}

					payloadMap := map[string]interface{}{
						"event":     "webhook.stress_test",
						"timestamp": time.Now().Format(time.RFC3339),
						"worker_id": workerID,
					}
					payloadBytes, _ := json.Marshal(payloadMap)

					reqBody := map[string]interface{}{
						"target_url": deliveryTarget,
						"payload":    json.RawMessage(payloadBytes),
					}
					reqBodyBytes, _ := json.Marshal(reqBody)

					req, err := http.NewRequest("POST", targetURL, bytes.NewBuffer(reqBodyBytes))
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
						continue
					}

					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("X-API-Key", apiKey)

					atomic.AddInt64(&sentCount, 1)
					resp, err := client.Do(req)
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
						continue
					}

					if resp.StatusCode == http.StatusAccepted {
						atomic.AddInt64(&successCount, 1)
					} else if resp.StatusCode == http.StatusTooManyRequests {
						atomic.AddInt64(&rateLimitCount, 1)
					} else {
						atomic.AddInt64(&errorCount, 1)
					}
					resp.Body.Close()
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	totalSent := atomic.LoadInt64(&sentCount)
	totalSuccess := atomic.LoadInt64(&successCount)
	totalRateLimits := atomic.LoadInt64(&rateLimitCount)
	totalErrors := atomic.LoadInt64(&errorCount)

	rps := float64(totalSent) / elapsed.Seconds()

	fmt.Printf("\n=========================================\n")
	fmt.Printf(" Webhook Stress Test Results\n")
	fmt.Printf("=========================================\n")
	fmt.Printf("Elapsed Time:         %.2fs\n", elapsed.Seconds())
	fmt.Printf("Total Requests Sent:  %d\n", totalSent)
	fmt.Printf("Accepted (202):       %d\n", totalSuccess)
	fmt.Printf("Rate Limited (429):   %d\n", totalRateLimits)
	fmt.Printf("Errors/Other:         %d\n", totalErrors)
	fmt.Printf("Throughput (RPS):     %.2f req/sec\n", rps)
	fmt.Printf("=========================================\n")
}
