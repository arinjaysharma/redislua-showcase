package wallet

import (
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

//go:embed script.lua
var scriptSrc string

var transferScript = redis.NewScript(scriptSrc)

type Service struct{ rdb *redis.Client }

func NewService(rdb *redis.Client) *Service { return &Service{rdb: rdb} }

func walletKey(id string) string { return "wallet:" + id }
func logKey(id string) string    { return "wallet:log:" + id }

func newTxnID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return fmt.Sprintf("txn_%x", b)
}

func (s *Service) TopUpHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			WalletID string `json:"wallet_id"`
			Amount   int64  `json:"amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Amount <= 0 {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		if req.WalletID == "" {
			req.WalletID = "default"
		}
		newBal, err := s.rdb.IncrBy(r.Context(), walletKey(req.WalletID), req.Amount).Result()
		if err != nil {
			http.Error(w, `{"error":"redis error"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"wallet_id": req.WalletID, "balance": newBal})
	}
}

func (s *Service) TransferHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			From   string `json:"from"`
			To     string `json:"to"`
			Amount int64  `json:"amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Amount <= 0 {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		if req.From == "" || req.To == "" || req.From == req.To {
			http.Error(w, `{"error":"from and to must differ"}`, http.StatusBadRequest)
			return
		}

		tid := newTxnID()
		raw, err := transferScript.Run(r.Context(), s.rdb,
			[]string{walletKey(req.From), walletKey(req.To), logKey(req.From)},
			req.Amount, tid).Int64Slice()
		if err != nil {
			http.Error(w, `{"error":"redis error"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if raw[0] == -1 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false, "from_balance": raw[1], "to_balance": 0,
				"amount": req.Amount, "message": "Insufficient funds",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true, "from_balance": raw[1], "to_balance": raw[2],
			"amount": req.Amount, "txn_id": tid,
			"message": fmt.Sprintf("Transferred %d from %s to %s", req.Amount, req.From, req.To),
		})
	}
}

func (s *Service) BalanceHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		bal, err := s.rdb.Get(r.Context(), walletKey(id)).Result()
		w.Header().Set("Content-Type", "application/json")
		if err == redis.Nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"wallet_id": id, "balance": 0})
			return
		}
		if err != nil {
			http.Error(w, `{"error":"redis error"}`, http.StatusInternalServerError)
			return
		}
		v, _ := strconv.ParseInt(bal, 10, 64)
		json.NewEncoder(w).Encode(map[string]interface{}{"wallet_id": id, "balance": v})
	}
}

func (s *Service) HistoryHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		entries, err := s.rdb.LRange(r.Context(), logKey(id), 0, 19).Result()
		if err != nil {
			http.Error(w, `{"error":"redis error"}`, http.StatusInternalServerError)
			return
		}
		type TxnEntry struct {
			TxnID     string `json:"txn_id"`
			Amount    int64  `json:"amount"`
			FromAfter int64  `json:"from_after"`
			ToAfter   int64  `json:"to_after"`
		}
		txns := make([]TxnEntry, 0, len(entries))
		for _, e := range entries {
			p := strings.Split(e, "|")
			if len(p) == 4 {
				amount, _ := strconv.ParseInt(p[1], 10, 64)
				fa, _ := strconv.ParseInt(p[2], 10, 64)
				ta, _ := strconv.ParseInt(p[3], 10, 64)
				txns = append(txns, TxnEntry{TxnID: p[0], Amount: amount, FromAfter: fa, ToAfter: ta})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"wallet_id": id, "transactions": txns})
	}
}
