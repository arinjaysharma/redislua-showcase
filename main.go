package main

import (
	"bufio"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/arinj/redislua-showcase/internal/flashsale"
	"github.com/arinj/redislua-showcase/internal/leaderboard"
	"github.com/arinj/redislua-showcase/internal/lock"
	"github.com/arinj/redislua-showcase/internal/ratelimit"
	rdb "github.com/arinj/redislua-showcase/internal/redis"
	"github.com/arinj/redislua-showcase/internal/wallet"
)

//go:embed web/*
var webFS embed.FS

func loadEnv() {
	f, err := os.Open(".env")
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			if os.Getenv(k) == "" {
				os.Setenv(k, v)
			}
		}
	}
}

func main() {
	loadEnv()
	client := rdb.New()

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	// Rate Limiter
	r.Get("/api/ping", ratelimit.PingHandler(client))
	r.Get("/api/ratelimit/status", ratelimit.StatusHandler(client))
	r.Delete("/api/ratelimit/reset", ratelimit.ResetHandler(client))
	r.Post("/api/ratelimit/benchmark", ratelimit.BenchmarkHandler(client))

	// Distributed Lock
	lockSvc := lock.NewService(client)
	r.Post("/api/lock/acquire", lockSvc.AcquireHandler())
	r.Post("/api/lock/release", lockSvc.ReleaseHandler())

	// Leaderboard
	hub := leaderboard.NewHub()
	lbSvc := leaderboard.NewService(client, hub)
	r.Post("/api/leaderboard/score", lbSvc.UpdateScoreHandler())
	r.Get("/api/leaderboard/top", lbSvc.TopHandler())
	r.Get("/api/leaderboard/rank/{player}", lbSvc.RankHandler())
	r.Post("/api/leaderboard/seed", lbSvc.SeedHandler())
	r.Delete("/api/leaderboard/reset", lbSvc.ResetHandler())
	r.Get("/api/leaderboard/stream", hub.StreamHandler())

	// Flash Sale
	fsSvc := flashsale.NewService(client)
	r.Post("/api/flash-sale/init", fsSvc.InitHandler())
	r.Post("/api/flash-sale/buy", fsSvc.BuyHandler())
	r.Get("/api/flash-sale/stock", fsSvc.StockHandler())
	r.Post("/api/flash-sale/stress", fsSvc.StressHandler())

	// Wallet
	walletSvc := wallet.NewService(client)
	r.Post("/api/wallet/topup", walletSvc.TopUpHandler())
	r.Post("/api/wallet/transfer", walletSvc.TransferHandler())
	r.Get("/api/wallet/balance/{id}", walletSvc.BalanceHandler())
	r.Get("/api/wallet/history/{id}", walletSvc.HistoryHandler())

	// Frontend (embedded)
	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}
	r.Handle("/*", http.FileServer(http.FS(webContent)))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("RedisLua Showcase running -> http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
