package leaderboard

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

//go:embed script.lua
var scriptSrc string

var script = redis.NewScript(scriptSrc)

const boardKey = "leaderboard:global"

type Service struct {
	rdb *redis.Client
	hub *Hub
}

func NewService(rdb *redis.Client, hub *Hub) *Service {
	return &Service{rdb: rdb, hub: hub}
}

type Entry struct {
	Rank   int    `json:"rank"`
	Player string `json:"player"`
	Score  int64  `json:"score"`
}

func toStringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	raw, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, len(raw))
	for i, item := range raw {
		switch x := item.(type) {
		case string:
			out[i] = x
		case int64:
			out[i] = strconv.FormatInt(x, 10)
		}
	}
	return out
}

func parseEntries(raw []string, startRank int) []Entry {
	entries := make([]Entry, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		score, _ := strconv.ParseInt(raw[i+1], 10, 64)
		entries = append(entries, Entry{
			Rank:   startRank + i/2 + 1,
			Player: raw[i],
			Score:  score,
		})
	}
	return entries
}

type ScoreResponse struct {
	Rank         int64   `json:"rank"`
	Score        int64   `json:"score"`
	TotalPlayers int64   `json:"total_players"`
	Top          []Entry `json:"top"`
	Neighbors    []Entry `json:"neighbors"`
}

func (s *Service) UpdateScoreHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Player string `json:"player"`
			Score  int64  `json:"score"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Player == "" {
			http.Error(w, `{"error":"player and score required"}`, http.StatusBadRequest)
			return
		}

		result, err := script.Run(r.Context(), s.rdb, []string{boardKey},
			req.Player, req.Score, 10, 2).Slice()
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
			return
		}

		rank := result[0].(int64)
		score := result[1].(int64)
		total := result[2].(int64)
		topRaw := toStringSlice(result[3])
		nbRaw := toStringSlice(result[4])

		nbStart := int(rank) - 3
		if nbStart < 0 {
			nbStart = 0
		}

		resp := ScoreResponse{
			Rank: rank, Score: score, TotalPlayers: total,
			Top:       parseEntries(topRaw, 0),
			Neighbors: parseEntries(nbRaw, nbStart),
		}

		s.hub.Broadcast(map[string]interface{}{
			"type": "score_update", "player": req.Player,
			"rank": rank, "score": score, "total_players": total,
			"top": resp.Top,
		})

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func (s *Service) TopHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := 10
		if ns := r.URL.Query().Get("n"); ns != "" {
			if p, err := strconv.Atoi(ns); err == nil && p > 0 {
				n = p
			}
		}
		raw, err := s.rdb.ZRevRangeWithScores(r.Context(), boardKey, 0, int64(n-1)).Result()
		if err != nil {
			http.Error(w, `{"error":"redis error"}`, http.StatusInternalServerError)
			return
		}
		entries := make([]Entry, len(raw))
		for i, z := range raw {
			entries[i] = Entry{Rank: i + 1, Player: z.Member.(string), Score: int64(z.Score)}
		}
		total, _ := s.rdb.ZCard(r.Context(), boardKey).Result()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"entries": entries, "total_players": total})
	}
}

func (s *Service) RankHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		player := chi.URLParam(r, "player")
		rank, err := s.rdb.ZRevRank(r.Context(), boardKey, player).Result()
		if err == redis.Nil {
			http.Error(w, `{"error":"player not found"}`, http.StatusNotFound)
			return
		}
		score, _ := s.rdb.ZScore(r.Context(), boardKey, player).Result()
		total, _ := s.rdb.ZCard(r.Context(), boardKey).Result()
		startIdx := rank - 2
		if startIdx < 0 {
			startIdx = 0
		}
		nbRaw, _ := s.rdb.ZRevRangeWithScores(r.Context(), boardKey, startIdx, rank+2).Result()
		nbs := make([]Entry, len(nbRaw))
		for i, z := range nbRaw {
			nbs[i] = Entry{Rank: int(startIdx) + i + 1, Player: z.Member.(string), Score: int64(z.Score)}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"rank": rank + 1, "score": int64(score), "total_players": total, "neighbors": nbs,
		})
	}
}

func (s *Service) ResetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.rdb.Del(r.Context(), boardKey)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"reset": true})
	}
}

var playerNames = []string{
	"alice", "bob", "charlie", "dave", "eve", "frank", "grace",
	"henry", "iris", "jack", "karen", "leo", "mia", "noah",
	"olivia", "peter", "quinn", "rachel", "sam", "tina",
	"ursula", "victor", "wendy", "xavier", "yara", "zoe",
}

func (s *Service) SeedHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Count int `json:"count"` }
		json.NewDecoder(r.Body).Decode(&req)
		if req.Count <= 0 {
			req.Count = 20
		}
		added := 0
		for i := 0; i < req.Count && i < len(playerNames); i++ {
			sc := rand.Int63n(10000) + 100
			s.rdb.ZAdd(r.Context(), boardKey, redis.Z{Score: float64(sc), Member: playerNames[i]})
			added++
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"seeded": added})
	}
}
