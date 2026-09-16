package flashsale

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:embed script.lua
var scriptSrc string

var script = redis.NewScript(scriptSrc)

const stockKey = "flashsale:stock"

type Service struct{ rdb *redis.Client }

func NewService(rdb *redis.Client) *Service { return &Service{rdb: rdb} }

type PurchaseResult struct {
	UserID    string `json:"user_id"`
	Success   bool   `json:"success"`
	Remaining int64  `json:"remaining"`
	Message   string `json:"message"`
}

func (s *Service) InitHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Stock int64 `json:"stock"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Stock <= 0 {
			http.Error(w, `{"error":"stock must be > 0"}`, http.StatusBadRequest)
			return
		}
		s.rdb.Set(r.Context(), stockKey, req.Stock, 0)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"stock": req.Stock,
			"message": fmt.Sprintf("Sale initialized with %d units", req.Stock),
		})
	}
}

func doBuy(ctx context.Context, rdb *redis.Client, userID string, qty int64) (*PurchaseResult, error) {
	raw, err := script.Run(ctx, rdb, []string{stockKey}, qty).Int64Slice()
	if err != nil {
		return nil, err
	}
	switch raw[0] {
	case -2:
		return &PurchaseResult{UserID: userID, Success: false, Message: "Sale not initialized"}, nil
	case -1:
		return &PurchaseResult{UserID: userID, Success: false, Remaining: raw[1], Message: "Out of stock"}, nil
	default:
		return &PurchaseResult{
			UserID: userID, Success: true, Remaining: raw[0],
			Message: fmt.Sprintf("Success! %d remaining", raw[0]),
		}, nil
	}
}

func (s *Service) BuyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UserID string `json:"user_id"`
			Qty    int64  `json:"qty"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.UserID == "" {
			req.UserID = "anonymous"
		}
		if req.Qty <= 0 {
			req.Qty = 1
		}
		res, err := doBuy(r.Context(), s.rdb, req.UserID, req.Qty)
		if err != nil {
			http.Error(w, `{"error":"redis error"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	}
}

func (s *Service) StockHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stock, err := s.rdb.Get(r.Context(), stockKey).Result()
		w.Header().Set("Content-Type", "application/json")
		if err == redis.Nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"stock": 0, "initialized": false})
			return
		}
		if err != nil {
			http.Error(w, `{"error":"redis error"}`, http.StatusInternalServerError)
			return
		}
		stockInt, _ := strconv.ParseInt(stock, 10, 64)
		json.NewEncoder(w).Encode(map[string]interface{}{"stock": stockInt, "initialized": true})
	}
}

type StressResponse struct {
	Attempted int64            `json:"attempted"`
	Succeeded int64            `json:"succeeded"`
	Failed    int64            `json:"failed"`
	Duration  string           `json:"duration"`
	Purchases []PurchaseResult `json:"purchases"`
}

func (s *Service) StressHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Buyers int   `json:"buyers"`
			Stock  int64 `json:"stock"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Buyers <= 0 {
			req.Buyers = 50
		}
		if req.Stock > 0 {
			s.rdb.Set(r.Context(), stockKey, req.Stock, 0)
		}

		start := time.Now()
		results := make([]PurchaseResult, req.Buyers)
		var wg sync.WaitGroup
		var mu sync.Mutex
		var succeeded, failed int64
		ctx := context.Background()

		for i := 0; i < req.Buyers; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				uid := fmt.Sprintf("user_%03d", idx+1)
				res, err := doBuy(ctx, s.rdb, uid, 1)
				if err != nil {
					res = &PurchaseResult{UserID: uid, Success: false, Message: "error"}
				}
				mu.Lock()
				results[idx] = *res
				if res.Success {
					succeeded++
				} else {
					failed++
				}
				mu.Unlock()
			}(i)
		}
		wg.Wait()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(StressResponse{
			Attempted: int64(req.Buyers), Succeeded: succeeded, Failed: failed,
			Duration:  fmt.Sprintf("%dms", time.Since(start).Milliseconds()),
			Purchases: results,
		})
	}
}
