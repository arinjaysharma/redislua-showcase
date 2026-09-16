package lock

import (
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:embed script.lua
var scriptSrc string

var unlockScript = redis.NewScript(scriptSrc)

type Service struct{ rdb *redis.Client }

func NewService(rdb *redis.Client) *Service { return &Service{rdb: rdb} }

func newToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

type AcquireRequest struct {
	Resource string `json:"resource"`
	Worker   string `json:"worker"`
	TTLMs    int    `json:"ttl_ms"`
}

type AcquireResponse struct {
	Acquired bool   `json:"acquired"`
	Token    string `json:"token,omitempty"`
	Resource string `json:"resource"`
	Worker   string `json:"worker"`
	TTLMs    int    `json:"ttl_ms"`
	Message  string `json:"message"`
}

type ReleaseRequest struct {
	Resource string `json:"resource"`
	Token    string `json:"token"`
	Worker   string `json:"worker"`
}

type ReleaseResponse struct {
	Released bool   `json:"released"`
	Worker   string `json:"worker"`
	Message  string `json:"message"`
}

func (s *Service) AcquireHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AcquireRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		if req.TTLMs <= 0 {
			req.TTLMs = 5000
		}
		if req.Resource == "" {
			req.Resource = "default"
		}
		if req.Worker == "" {
			req.Worker = "Worker"
		}

		key := "lock:" + req.Resource
		tok := newToken()
		ttl := time.Duration(req.TTLMs) * time.Millisecond

		ok, err := s.rdb.SetNX(r.Context(), key, tok, ttl).Result()
		if err != nil {
			http.Error(w, `{"error":"redis error"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if ok {
			json.NewEncoder(w).Encode(AcquireResponse{
				Acquired: true, Token: tok,
				Resource: req.Resource, Worker: req.Worker, TTLMs: req.TTLMs,
				Message: fmt.Sprintf("%s acquired the lock", req.Worker),
			})
		} else {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(AcquireResponse{
				Acquired: false,
				Resource: req.Resource, Worker: req.Worker,
				Message: "Lock is held by another client",
			})
		}
	}
}

func (s *Service) ReleaseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ReleaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}

		key := "lock:" + req.Resource
		result, err := unlockScript.Run(r.Context(), s.rdb, []string{key}, req.Token).Int()
		if err != nil && err != redis.Nil {
			http.Error(w, `{"error":"redis error"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if result == 1 {
			json.NewEncoder(w).Encode(ReleaseResponse{
				Released: true, Worker: req.Worker,
				Message: fmt.Sprintf("%s released the lock", req.Worker),
			})
		} else {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(ReleaseResponse{
				Released: false, Worker: req.Worker,
				Message: "Release failed: token mismatch or lock already expired",
			})
		}
	}
}
