package ratelimit

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:embed script.lua
var scriptSrc string

//go:embed token_bucket.lua
var tokenBucketSrc string

var script = redis.NewScript(scriptSrc)
var tokenBucketScript = redis.NewScript(tokenBucketSrc)

const (
	WindowMs = 60 * 1000
	Limit    = 10
)

type Response struct {
	Allowed        bool   `json:"allowed"`
	Remaining      int64  `json:"remaining"`
	ResetAt        int64  `json:"reset_at"`
	ResetInSeconds int64  `json:"reset_in_seconds"`
	Message        string `json:"message"`
}

func uid() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func PingHandler(rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := "ratelimit:" + clientIP(r)
		now := time.Now().UnixMilli()

		slice, err := script.Run(r.Context(), rdb, []string{key},
			now, WindowMs, Limit, uid()).Slice()
		if err != nil {
			log.Printf("ratelimit script error: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(fmt.Sprintf(`{"error":"%v"}`, err)))
			return
		}

		var allowed bool
		var remaining, resetAt int64

		if len(slice) >= 3 {
			if v, ok := slice[0].(int64); ok {
				allowed = v == 1
			}
			if v, ok := slice[1].(int64); ok {
				remaining = v
			}
			switch v := slice[2].(type) {
			case int64:
				resetAt = v
			case float64:
				resetAt = int64(v)
			case string:
				fmt.Sscanf(v, "%d", &resetAt)
			}
		}

		resetIn := (resetAt - now) / 1000
		if resetIn < 0 {
			resetIn = 0
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", Limit))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt))

		msg := "Request allowed"
		if !allowed {
			msg = fmt.Sprintf("Rate limited. Retry in %ds", resetIn)
			w.WriteHeader(http.StatusTooManyRequests)
		}

		json.NewEncoder(w).Encode(Response{
			Allowed:        allowed,
			Remaining:      remaining,
			ResetAt:        resetAt,
			ResetInSeconds: resetIn,
			Message:        msg,
		})
	}
}

func StatusHandler(rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := "ratelimit:" + clientIP(r)
		now := time.Now().UnixMilli()
		windowStart := now - WindowMs

		count, _ := rdb.ZCount(r.Context(), key,
			fmt.Sprintf("%d", windowStart), "+inf").Result()
		remaining := int64(Limit) - count
		if remaining < 0 {
			remaining = 0
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"limit":            Limit,
			"used":             count,
			"remaining":        remaining,
			"reset_at":         now + WindowMs,
			"reset_in_seconds": WindowMs / 1000,
			"window_ms":        WindowMs,
		})
	}
}

func ResetHandler(rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := "ratelimit:" + clientIP(r)
		rdb.Del(r.Context(), key)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"limit":            Limit,
			"used":             0,
			"remaining":        Limit,
			"reset_at":         time.Now().UnixMilli() + WindowMs,
			"reset_in_seconds": WindowMs / 1000,
			"window_ms":        WindowMs,
			"message":          "Rate limit reset successfully",
		})
	}
}

type BenchmarkRequest struct {
	TotalRequests int    `json:"total_requests"`
	Workers       int    `json:"workers"`
	RateLimit     int    `json:"rate_limit"`
	Algorithm     string `json:"algorithm"`
}

type BenchmarkResponse struct {
	TotalRequests int    `json:"total_requests"`
	Allowed       int64  `json:"allowed"`
	Denied        int64  `json:"denied"`
	Errors        int64  `json:"errors"`
	DurationMs    int64  `json:"duration_ms"`
	RPS           int64  `json:"rps"`
	Algorithm     string `json:"algorithm"`
	Message       string `json:"message"`
}

func BenchmarkHandler(rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req BenchmarkRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.TotalRequests <= 0 {
			req.TotalRequests = 100000
		}
		if req.Workers <= 0 {
			req.Workers = 150
		}
		if req.RateLimit <= 0 {
			req.RateLimit = 50000
		}
		if req.Algorithm == "" {
			req.Algorithm = "token_bucket"
		}

		ctx := context.Background()
		benchKey := fmt.Sprintf("benchmark:ratelimit:%d", time.Now().UnixNano())
		rdb.Del(ctx, benchKey)
		defer rdb.Del(ctx, benchKey)

		jobs := make(chan int, req.TotalRequests)
		for i := 0; i < req.TotalRequests; i++ {
			jobs <- i
		}
		close(jobs)

		var allowedCount int64
		var deniedCount int64
		var errorCount int64

		start := time.Now()
		var wg sync.WaitGroup

		for wIdx := 0; wIdx < req.Workers; wIdx++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range jobs {
					nowUs := time.Now().UnixMicro()
					// burst capacity is 10% of rate limit or min 1000 to trigger rate limiting under high concurrency
					capacity := req.RateLimit / 5
					if capacity < 1000 {
						capacity = 1000
					}
					res, err := tokenBucketScript.Run(ctx, rdb, []string{benchKey},
						req.RateLimit, capacity, nowUs, 1).Slice()
					if err != nil {
						atomic.AddInt64(&errorCount, 1)
						continue
					}
					if len(res) > 0 {
						if code, ok := res[0].(int64); ok && code == 1 {
							atomic.AddInt64(&allowedCount, 1)
						} else {
							atomic.AddInt64(&deniedCount, 1)
						}
					}
				}
			}()
		}

		wg.Wait()
		duration := time.Since(start)
		durationMs := duration.Milliseconds()
		if durationMs == 0 {
			durationMs = 1
		}
		rps := int64(float64(req.TotalRequests) / duration.Seconds())

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(BenchmarkResponse{
			TotalRequests: req.TotalRequests,
			Allowed:       allowedCount,
			Denied:        deniedCount,
			Errors:        errorCount,
			DurationMs:    durationMs,
			RPS:           rps,
			Algorithm:     "O(1) Token Bucket (Lua in Redis)",
			Message:       fmt.Sprintf("Processed %d operations in %v (Achieved: %d ops/sec)", req.TotalRequests, duration.Round(time.Millisecond), rps),
		})
	}
}