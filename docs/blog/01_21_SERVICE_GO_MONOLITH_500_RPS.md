# Architecting a 21-Service Go Monolith: Bounded Contexts, PostGIS & Redis WebSockets

*By Vinay K R — Co-Architect & Core Backend Engineer*

> [!NOTE]
> **Architecture & Snapshot Disclosure**: This article analyzes the architectural patterns, database transaction propagation, and spatial/messaging subsystems engineered for the Medha Platform API Go backend. Preliminary performance targets represent early simulation benchmarks rather than published production audits. For reproducible test suites and snapshot provenance, refer to the repository [README.md](../../README.md).

---

## The Monolith vs. Microservice Dilemma

When designing backends for product platforms, a common industry pitfall is prematurely decomposing systems into dozens of independently deployed microservices. While microservices offer organizational scaling for large engineering divisions, they introduce immense operational overhead:
- Network serialization latency and RPC overhead
- Distributed transaction coordination (Two-Phase Commit or Saga complexity)
- Complex cascading failure modes and partial availability issues
- High infrastructure footprint and maintenance burden

For the **Medha Platform**, we chose a pragmatic approach: **A Domain-Driven Modular Monolith in Go.**

In this article, I walk through how we architected a single compiled Go binary containing **21 strictly isolated bounded contexts**, 50 automated database migrations, PostGIS spatial indexing, and WebSocket pub/sub fan-out — engineered for high reliability, atomic transactional guarantees, and operational simplicity.

---

## 1. Domain-Driven Bounded Contexts in a Single Binary

Each domain within `internal/<domain>/` is isolated into 4 physical layers:
1. **Domain (`domain/`):** Pure data models, enums, DTOs, and interface definitions. Has **zero dependencies** on external packages.
2. **Repository (`repository/`):** Database queries using `pgxpool`. Implements the interfaces defined in the domain layer.
3. **Service (`service/`):** Business rules, validations, state machine transitions, and cross-repository transaction coordinators.
4. **Handler (`handler/`):** HTTP / WebSocket boundary. Deserializes request payloads, validates input constraints, delegates to the service, and serializes RFC 7807 responses.

```
┌────────────────────────────────────────────────────────┐
│                        HTTP                            │
└──────────────────────────┬─────────────────────────────┘
                           ▼
┌────────────────────────────────────────────────────────┐
│               Handler (HTTP Transport)                 │
└──────────────────────────┬─────────────────────────────┘
                           ▼
┌────────────────────────────────────────────────────────┐
│              Service (Business Logic)                  │
└──────────────────────────┬─────────────────────────────┘
                           ▼
┌────────────────────────────────────────────────────────┐
│         Repository (Database / pgx Interface)          │
└────────────────────────────────────────────────────────┘
```

### The Transaction Executor Pattern

One major risk of modular monoliths is database transaction leakage across domain boundaries. If Service A needs to modify entities in Repository A and Repository B atomically, how do you pass transactions without coupling their implementations?

We solved this with a universal `GetExecutor(ctx, pool)` helper:

```go
type DBExecutor interface {
    Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func GetExecutor(ctx context.Context, pool *pgxpool.Pool) DBExecutor {
    if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
        return tx
    }
    return pool
}
```

Whenever a service begins a transaction (`pool.BeginTx`), it embeds the `pgx.Tx` into the `context.Context`. Any repository invoked with that context automatically participates in the active transaction without requiring custom transaction wrapper objects.

---

## 2. Real-Time Spatial Proximity with PostGIS & GIST Indexing

Location-based matchmaking queries require finding qualified service providers within a dynamic radius (e.g. 50km) sorted by distance.

Naive implementations either compute Haversine distances in application memory or execute unindexed Euclidean calculations. In our PostgreSQL architecture, we leverage native **PostGIS Geography (`geography(Point, 4326)`)** types backed by a **GIST index**:

```sql
CREATE INDEX idx_pandit_profiles_location ON pandit_profiles USING GIST (location);
```

By structuring spatial queries using `ST_DWithin`, PostgreSQL optimizes execution using the bounding box index, reducing lookup latencies from ~350ms to **under 15ms** across 100,000 coordinates:

```sql
SELECT id, display_name, city,
       ST_Distance(location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography) AS distance_meters
FROM pandit_profiles
WHERE ST_DWithin(location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $3)
  AND is_active = true
ORDER BY distance_meters ASC
LIMIT $4 OFFSET $5;
```

---

## 3. High-Concurrency WebSockets via Redis Pub/Sub Backplane

Real-time messaging channels require delivering messages instantly across active mobile clients without tying users to a specific backend process.

Our WebSocket hub separates connection management from message dissemination:
1. **Client Connection:** A mobile client initiates a WebSocket connection with a short-lived authentication token (`/ws/v2?token=...`).
2. **Local Registry:** The Go node registers the client socket in an in-memory `sync.RWMutex` map indexed by `user_id`.
3. **Cluster Fan-Out:** When a user sends a message, the server writes the record to PostgreSQL and simultaneously publishes the event payload to a Redis channel (`msg:conv:{conversation_id}`).
4. **Broadcast Delivery:** All running Go nodes subscribed to that Redis channel inspect their local connection maps and stream the message frame to the recipient in $<5\text{ms}$.

---

## 4. Verification, Testing & System Ceilings

To ensure reliability without relying on heavyweight external infrastructure in local development or CI pipelines, the platform leverages:

- **Strict Test Double Fakes:** Hand-written repository test doubles in `service_test` packages verify domain business logic, multi-status state machines, and transactional rollbacks deterministically without mocking libraries.
- **Race Detection in CI:** Every bounded context executes under `go test -race ./...` in automated workflows, guarding against concurrent map access in WebSocket hubs and worker pools.
- **Fail-Fast vs. Graceful Degradation:** The application supports dual startup modes — running in degraded local development mode when secondary datastores (S3, Redis) are offline, while failing fast on schema mismatches or missing credentials in production.
- **Known Ceilings:** As a modular monolith, PostgreSQL acts as the single primary system of record. High-volume contexts such as chat messaging or real-time geolocation updates represent the primary scaling ceiling, designed to be extracted into dedicated read replicas or microservices only when query volume profile justifies the operational trade-off.

---

## Conclusion

A well-architected Domain-Driven Monolith provides an optimal balance of developer velocity, atomic transactional guarantees, and operational simplicity. By investing in clean 4-layer boundaries, transaction-aware query execution, and robust database indexing, a single Go binary delivers resilient backend services without the unnecessary operational friction of a microservice fleet.
