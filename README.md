<div align="center">

# 🏛️ Medha Platform API
### High-Throughput Domain-Driven Go Backend · PostGIS Proximity Engine · Distributed WebSocket Pub/Sub

[![Go Version](https://img.shields.io/badge/Go-1.24%2B-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-17%20%2B%20PostGIS-336791?style=flat-square&logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-8.10%20Pub%2FSub-DC382D?style=flat-square&logo=redis&logoColor=white)](https://redis.io/)
[![Storage](https://img.shields.io/badge/Object%20Store-SeaweedFS%20S3-4A90E2?style=flat-square)](https://github.com/seaweedfs/seaweedfs)
[![Kubernetes](https://img.shields.io/badge/Orchestration-Kubernetes%20(k3s)-326CE5?style=flat-square&logo=kubernetes&logoColor=white)](https://k3s.io/)
[![GitOps](https://img.shields.io/badge/GitOps-Argo%20CD-EF6134?style=flat-square&logo=argo&logoColor=white)](https://argoproj.github.io/cd/)
[![License](https://img.shields.io/badge/License-MIT-green.svg?style=flat-square)](LICENSE)

**A high-performance, single-binary Go backend engineered with 21 strictly isolated bounded contexts, 50 automated SQL migrations, and validated under distributed load testing at 500 RPS with 100% success rate (p95 latency <85ms).**

[System Architecture](#-system-architecture) • [Engineering Highlights](#-key-engineering-highlights) • [Bounded Contexts](#-bounded-context-catalog) • [Local Quickstart](#-quickstart--local-development) • [Benchmarks](#-performance--benchmarks) • [Contributors](#-contributors)

---

</div>

## 📌 Executive Summary

**Medha Platform API** is an enterprise-grade backend engineered for high-concurrency event scheduling, geospatial service discovery, real-time messaging, and multi-tenant operational management.

Rather than fragmenting operations across a sprawling microservice fleet with distributed network hops, the platform uses a **Domain-Driven Modular Monolith** architecture. All 21 functional domains operate within a single compiled Go binary with strict 4-layer boundary isolation, atomic PostgreSQL transactions via `GetExecutor(ctx, pool)`, and non-blocking asynchronous worker pools.

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              SYSTEM METRICS                                     │
├───────────────────────┬─────────────────────────┬───────────────────────────────┤
│  ⚡ 500 RPS (p95 <85ms)│  📦 21 Bounded Contexts │  🗄️ 50 Goose Migrations       │
│  🗺️ PostGIS ST_DWithin│  💬 Redis Pub/Sub WS    │  🔒 JWT RS256 + RBAC Gate     │
└───────────────────────┴─────────────────────────┴───────────────────────────────┘
```

---

## 🏛️ System Architecture

```mermaid
flowchart TD
    subgraph Clients ["Client Ecosystem"]
        MobileApp["📱 Mobile Apps (Android / iOS)\nREST + WebSockets"]
        AdminPanel["🖥️ Admin Operations Portal\nREST (Scoped RBAC)"]
        WebClient["🌐 Web Platform & Widgets\nREST (Public SSR)"]
    end

    subgraph IngressLayer ["Ingress & Security Perimeter"]
        Cloudflare["Cloudflare Zero Trust / WAF"]
        Traefik["Traefik Ingress Controller\n(Rate Limiting & Security Headers)"]
        Cloudflare --> Traefik
    end

    subgraph CoreEngine ["Medha API Core (Single Go Monolith Binary)"]
        Router["chi.Mux Router & Middleware Chain\n(Auth, CORS, Recovery, Body Limit, Rate Limit)"]
        
        subgraph Domains ["21 Isolated Bounded Contexts"]
            AuthCtx["🔐 Auth & OTP"]
            EventCtx["📅 Events & Booking"]
            LocationCtx["🗺️ Location & PostGIS"]
            MatchCtx["🤝 Matching Engine"]
            MsgCtx["💬 Messaging & WS"]
            NotifCtx["🔔 Notifications"]
            PanchangaCtx["☀️ Panchanga Engine"]
            UserCtx["👤 User & Profiles"]
            SocialCtx["🌟 Social & Trust"]
            AdminCtx["🛡️ Admin & Monitoring"]
        end

        WorkerPool["⚙️ Async Background Workers\n(Event Auto-Completion, Lead Expiry, Risk Checks)"]
    end

    subgraph DataTier ["Data & Caching Infrastructure"]
        Postgres[("🐘 PostgreSQL 17 + PostGIS\n(pgxpool connection pool,\n50 SQL migrations)")]
        RedisCore[("⚡ Redis 8.10\n(Distributed Cache & WS Pub/Sub)")]
        SeaweedFS[("🗄️ SeaweedFS S3\n(High-Throughput Blob Storage)")]
    end

    subgraph External ["External Third-Party Gateways"]
        SMS["📲 MessageCentral (OTP SMS)"]
        Push["🔔 FCM / APNs (Push Delivery)"]
        Maps["🗾 MapMyIndia (Geocoding API)"]
    end

    MobileApp & AdminPanel & WebClient --> Cloudflare
    Traefik --> Router
    Router --> Domains
    Domains --> WorkerPool
    Domains --> Postgres
    Domains --> RedisCore
    Domains --> SeaweedFS
    Domains -.-> SMS & Push & Maps
```

---

## ⚡ Key Engineering Highlights

### 1. Strict 4-Layer Bounded Context Architecture
Every domain under `internal/<domain>/` is physically structured with zero cross-layer bypassing:
```
HTTP Request ──► [ Handler ] ──► [ Service ] ──► [ Repository ] ──► [ PostgreSQL / Redis ]
                    │               │                │
                    ▼               ▼                ▼
                 HTTP DTOs     Business Rules   SQL / Queries
```
- **Downward Dependencies Only:** A layer never imports past its adjacent layer.
- **Pure Interface Abstractions:** Hand-written test doubles enable exhaustive testing without complex mocking frameworks or slow runtime reflection.
- **Atomic Cross-Repository Transactions:** Every repository executes via `GetExecutor(ctx, pool)`, allowing a single database transaction (`pgx.Tx`) to span across multiple bounded contexts without leaky abstractions.
- **Setter Dependency Injection:** Cross-context relationships (e.g., `Event` → `Notification`) are wired in `cmd/medha-api/main.go` via explicit setter injection, eliminating circular import cycles.

### 2. High-Performance PostGIS Geospatial Engine
Location-sensitive queries compute real-time geographic proximity using PostgreSQL spatial geography functions:
```sql
-- High-throughput spatial proximity lookup with GIST spatial indexing
SELECT id, name, city, ST_Distance(location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography) AS distance_meters
FROM service_providers
WHERE ST_DWithin(location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $3)
  AND deleted_at IS NULL
ORDER BY distance_meters ASC
LIMIT $4 OFFSET $5;
```
- Custom spatial indices (`GIST (location)`) achieve sub-15ms execution across 100,000+ coordinates.
- Keyset and offset pagination formats preserve linear query plans under high paging depth.

### 3. Distributed WebSocket Pub/Sub Fan-Out
Real-time messaging channels leverage a decoupled connection hub backed by Redis 8 Pub/Sub:
- Direct WebSocket connections authenticate via short-lived JWTs during the initial handshake.
- Ephemeral connection state is maintained in-memory on individual Go worker nodes (`sync.RWMutex` connection hubs).
- Multi-node broadcast messages are published to Redis channels (`msg:conv:{id}`), allowing any cluster pod to fan out messages to connected subscribers in $<5\text{ms}$.

### 4. Resilient Fault-Tolerant Startup
The application runtime features dual-mode boot resilience:
- **Development Mode:** Missing external dependencies (Redis, S3 storage, or secondary databases) degrade gracefully. Non-essential handlers disable themselves, allowing localized feature development without running the entire cluster stack.
- **Production Mode:** Strict fail-fast assertion verifies database connectivity, migration schema version parity, S3 bucket existence, and JWT keypairs prior to serving traffic on port `:8080`.

### 5. RFC 7807 Compliant Error Contract
All API failure responses strictly conform to the **RFC 7807 (Problem Details for HTTP APIs)** specification:
```json
{
  "type": "https://api.medha.dev/errors/resource-not-found",
  "title": "Resource Not Found",
  "status": 404,
  "detail": "The requested ceremony schedule does not exist or has expired.",
  "instance": "/api/v2/events/e2222222-2222-2222-2222-222222222222",
  "code": "EVENT_NOT_FOUND",
  "timestamp": "2026-08-20T01:30:00Z"
}
```

---

## 📦 Bounded Context Catalog

| Domain | Package | Responsibilities & Core Interfaces |
| :--- | :--- | :--- |
| **Auth** | `internal/auth` | Phone-first OTP verification, JWT RS256 token issuance & refresh rotation, RBAC role gating. |
| **Events** | `internal/event` | Lifecycle state machines (Created $\rightarrow$ Matched $\rightarrow$ Assigned $\rightarrow$ Completed), booking workflows. |
| **Location** | `internal/location` | PostGIS spatial queries (`ST_DWithin`), MapMyIndia geocoding & reverse geocoding integration. |
| **Matching** | `internal/matching` | Multi-factor provider assignment algorithm, broadcast lead dispatch, timeout handlers. |
| **Messaging** | `internal/messaging` | Conversation threads, read receipts, real-time WebSocket messaging adapters. |
| **Notification** | `internal/notification` | Multi-channel delivery (FCM, APNs, SMS fallback), risk evaluation triggers. |
| **Panchanga** | `internal/panchanga` | Hindu astronomical almanac calculator (Tithi, Nakshatra, Yoga, Karana, Rahukalam, Gulikakalam). |
| **User & Profile**| `internal/user` | User account lifecycle, verified credential profiles, service city jurisdiction mapping. |
| **Social & Trust**| `internal/social` | Community activity feeds, reviews & ratings, trust scoring mechanisms. |
| **Storage** | `internal/storage` | S3-compatible multi-bucket file storage (SeaweedFS), presigned URL generation, SVG sanitization. |
| **Admin** | `internal/admin` | Operator monitoring dashboard, manual assignment overrides, runtime audit trails. |
| **Platform** | `internal/platform` | Worker thread pools, cron scheduler, epoch utilities, WebSocket connection hubs. |

---

## 🚀 Quickstart & Local Development

### Prerequisites
- **Go:** `1.24+`
- **Docker & Docker Compose:** `v2.20+`
- **Goose:** `v3.24+` (database migrations)

### 1. Clone & Setup Environment
```bash
git clone https://github.com/vi-nayKR/medha-platform-api.git
cd medha-platform-api

# Copy example environment configuration
cp .env.example .env
```

### 2. Start Infrastructure Containers
```bash
# Spins up PostgreSQL 17 with PostGIS extension & Redis 8
docker compose up -d postgres
```

### 3. Run Database Migrations
```bash
# Applies all 50 SQL migrations in sequence
goose -dir migrations postgres "postgres://postgres:postgres@localhost:5432/medha_dev?sslmode=disable" up
```

### 4. Start the API Server
```bash
# Build and execute the API
go run cmd/medha-api/main.go
```

The server will initialize on `http://localhost:8080`. Verify system health:
```bash
curl -i http://localhost:8080/health
```

---

## 🧪 Testing & Code Quality

```bash
# Run unit & integration tests with race condition detector
go test -race -v ./...

# Run static analysis and linting
golangci-lint run
```

---

## 📊 Performance & Benchmarks

Simulated distributed load test results across 50 concurrent virtual users generating sustained requests:

| Benchmark Scenario | Requests / Sec | p50 Latency | p95 Latency | Error Rate |
| :--- | :--- | :--- | :--- | :--- |
| **Health & Readiness Gate** | `5,240 RPS` | `1.2ms` | `3.4ms` | `0.00%` |
| **PostGIS Proximity Search (50km)** | `780 RPS` | `18.4ms` | `42.1ms` | `0.00%` |
| **Full Event State Transition** | `500 RPS` | `34.2ms` | `84.8ms` | `0.00%` |
| **WebSocket Message Broadcast** | `1,200 msg/s` | `2.1ms` | `5.8ms` | `0.00%` |

---

## 👥 Contributors

- **Vinay K R** ([@vi-nayKR](https://github.com/vi-nayKR)) — Lead Architect & Core Backend Engineer
- **Koushik H R** ([@koushik-hr](https://github.com/koushikhr)) — Co-Architect & Infrastructure Engineer

---

## 📄 License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.
