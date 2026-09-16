# RedisLua Showcase 🚀

An end-to-end interactive showcase demonstrating **why**, **when**, and **how** to use **Redis Lua Scripting** with **Go (Golang)**. 

Designed specifically as a demonstration project to showcase clean architecture, distributed systems concepts, race condition prevention, and full-stack execution to recruiters and interviewers.

---

## 🌟 Highlights

- **Single Binary Deployment:** Go backend serves REST APIs, Server-Sent Events (SSE), and embeds the interactive frontend via `embed.FS`.
- **Atomic Operations in Action:** Solves real-world distributed state challenges where standard Redis commands or multi-step operations fail under concurrency.
- **Visual & Interactive:** Includes a modern, responsive UI with live counters, real-time rank updates, race animations, and embedded Lua source code inspection.

---

## 🛠️ Tech Stack

- **Backend:** Go 1.22 (`go-chi/chi/v5`, `redis/go-redis/v9`)
- **Database:** Redis 7 (Alpine)
- **Frontend:** HTML5, Vanilla JavaScript (ES6+), Tailwind CSS (CDN), Server-Sent Events (SSE)
- **Containerization:** Docker & Docker Compose (Multi-stage build)

---

## 🎯 5 Real-World Demo Modules

### 1. 🚦 Sliding-Window Rate Limiter
- **Problem:** Fixed-window rate limiters suffer from burst boundaries. Naive checks (`GET` → check → `INCR` → `EXPIRE`) involve network latency and race conditions.
- **Lua Solution:** Uses a Redis Sorted Set (`ZSET`). In a single atomic execution, evicts timestamps older than the sliding window (`ZREMRANGEBYSCORE`), evaluates the count (`ZCARD`), adds the new request (`ZADD`), and updates the TTL (`PEXPIRE`).

### 2. 🔒 Distributed Lock (Token-Safe Release)
- **Problem:** Releasing a lock via simple `DEL key` can release a lock owned by another process if the original lock TTL expired mid-computation.
- **Lua Solution:** Verifies the ownership token matches the lock's value before issuing `DEL`. Eliminates the risk of deleting an alien lock without requiring complex client-side coordination.

### 3. 🏆 Real-Time Leaderboard with Consistent Rank Snapshots
- **Problem:** Updating scores and fetching current ranks + nearby competitors across multiple commands results in inconsistent rank views if other scores change simultaneously.
- **Lua Solution:** Updates the score (`ZADD`), retrieves the precise rank (`ZREVRANK`), grabs top competitors (`ZREVRANGE`), and fetches immediate neighbors in one atomic snapshot. Broadcasts changes in real-time over SSE.

### 4. ⚡ Flash Sale Oversell Prevention
- **Problem:** High concurrency causes classic inventory overselling ("check-then-act" race condition).
- **Lua Solution:** Collapses stock verification and decrement into an uninterruptible atomic script. 50+ concurrent requests hitting 10 items will allow exactly 10 successes and reject the rest without Go-side mutex contention.

### 5. 💡 Atomic Multi-Key Wallet Transfer
- **Problem:** Transferring funds between two keys (`DECRBY` user A, `INCRBY` user B) must be completely transactional. Failures or network disruptions midway leave funds in limbo.
- **Lua Solution:** Ensures strict atomicity across sender balance verification, debiting, crediting, and transaction history logging (`LPUSH`). Guarantees no negative balances or partial updates.

---

## 🚀 Quick Start (Docker Compose)

Ensure you have Docker and Docker Compose installed:

```bash
docker compose up --build
```

Once running, navigate to:
👉 **[http://localhost:8080](http://localhost:8080)**

---

## 📁 Project Structure

```
redislua-showcase/
├── cmd/server/
├── internal/
│   ├── flashsale/       # Flash sale handler & atomic stock script
│   ├── leaderboard/     # Leaderboard, SSE hub & atomic rank script
│   ├── lock/            # Distributed lock handler & token-safe unlock script
│   ├── ratelimit/       # Sliding-window rate limiter & ZSET script
│   ├── redis/           # Redis client singleton & health checks
│   └── wallet/          # Atomic wallet transfer & transaction log script
├── web/                 # Frontend UI embedded into Go binary via embed.FS
│   ├── index.html
│   ├── app.js
│   └── style.css
├── Dockerfile           # Multi-stage minimal runtime container
├── docker-compose.yml   # Redis + App orchestration
├── go.mod
└── main.go              # Entrypoint and API routing
```
