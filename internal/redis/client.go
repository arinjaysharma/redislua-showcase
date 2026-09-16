package redisclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func New() *redis.Client {
	var opt *redis.Options

	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		parsed, err := redis.ParseURL(redisURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid REDIS_URL format: %v\n", err)
			os.Exit(1)
		}
		opt = parsed
	} else {
		addr := os.Getenv("REDIS_ADDR")
		if addr == "" {
			addr = "localhost:6379"
		}
		opt = &redis.Options{
			Addr:     addr,
			Password: os.Getenv("REDIS_PASSWORD"),
		}
	}

	// Auto-enable TLS for Upstash or if explicitly requested
	if opt.TLSConfig == nil && (strings.Contains(opt.Addr, "upstash.io") || strings.HasPrefix(os.Getenv("REDIS_URL"), "rediss://")) {
		opt.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	opt.DialTimeout = 10 * time.Second
	opt.ReadTimeout = 5 * time.Second
	opt.WriteTimeout = 5 * time.Second
	opt.PoolSize = 50
	opt.MinIdleConns = 10

	rdb := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Redis connection failed (%s): %v\n", opt.Addr, err)
		os.Exit(1)
	}

	fmt.Printf("Connected to Redis at %s (PoolSize: %d)\n", opt.Addr, opt.PoolSize)
	return rdb
}
