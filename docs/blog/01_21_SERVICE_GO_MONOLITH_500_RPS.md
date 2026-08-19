# Building a 21-Service Go Monolith That Handles 500 RPS on a Single Node

*By Vinay K R — Lead Architect & Systems Engineer*

---

## The Monolith vs. Microservice Dilemma

When designing high-throughput backends for early-stage and growth platforms, the default industry temptation is often to prematurely decompose systems into dozens of independently deployed microservices. While microservices offer organizational scaling for large engineering divisions, they introduce immense operational complexity:
- Network serialization overhead (JSON/gRPC hops)
- Distributed transaction coordination (Two-Phase Commit or Saga patterns)
- Complex failure modes (partial outages, cascading timeouts)
- Expensive infrastructure footprints (multiple running VMs and container clusters)

For the **Medha Platform**, we chose a different path: **A Domain-Driven Modular Monolith in Go.**

In this article, I walk through how we architected a single compiled Go binary containing **21 strictly isolated bounded contexts**, 50 database migrations, PostGIS spatial lookups, and WebSocket pub/sub fan-out — capable of sustaining **500 RPS at p95 latency <85ms** on modest commodity hardware.

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

## 4. Benchmark Results: 500 RPS Under Distributed Load

We benchmarked the compiled binary against a PostgreSQL 17 + PostGIS instance running under container constraints (2 CPU cores, 2GB RAM):

- **Read Proximity Searches:** 780 requests/second at p95 latency of 42.1ms.
- **Transactional State Transitions:** 500 requests/second with 100% success rate (0 errors across 50,000 iterations).
- **Memory Footprint:** The idle Go monolith consumes just **42MB RAM**; under sustained 500 RPS load, RSS stabilized at **185MB RAM**.

---

## Conclusion

A well-architected Domain-Driven Monolith provides the perfect balance of developer velocity, atomic transactional guarantees, and massive throughput efficiency. By investing in clean 4-layer boundaries and robust database indexing, a single Go binary can easily handle enterprise-scale traffic at a fraction of the cost and complexity of a microservice fleet.
