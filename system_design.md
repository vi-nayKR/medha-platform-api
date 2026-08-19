# Medha API — System Design

**Scope.** This document captures *why* the API is built the way it is: goals, requirements, key
design decisions and their trade-offs, representative data flows, and the quality attributes
(scalability, reliability, security) the design targets. For the *as-built* component/layer map, see
[`architecture.md`](./architecture.md). For commands and conventions, see [`AGENTS.md`](./AGENTS.md).

---

## 1. Goals & non-goals

**Current state (2026-07-22):** the design below is implemented end to end across the production
and development environments. Android, iOS, admin, and web consumers share the deployed V2 API;
remaining iOS work is incremental product polish rather than a missing system integration.

**Goals**
- One backend that serves the mobile clients and the admin panel with a single coherent REST + WS
  API and a single relational system of record.
- Fast, safe iteration by a small team: strict layering so a change is local, and a test strategy
  that needs no mocking framework.
- Run cheaply on **one self-hosted cluster** with clean dev/prod isolation.

**Non-goals**
- Not a microservice fleet. Contexts are bounded *inside one process*; we accept the monolith's
  scaling ceiling in exchange for operational simplicity (see §6).
- Not multi-region or multi-tenant. Single primary datastore per environment.
- Not an event-sourced system; Postgres rows are the source of truth.

## 2. Requirements

| Type | Requirement |
| --- | --- |
| Functional | OTP auth; profile mgmt (Yajman/Pandit); booking/event lifecycle; matching & leads; interests; real-time messaging; notifications & push; social feed; panchanga; admin ops. |
| Availability | Best-effort single-cluster; graceful local degradation; production fails fast on missing infra. |
| Latency | Interactive REST p95 < ~300 ms; WebSocket delivery near-real-time. |
| Consistency | Strong within Postgres (transactions); at-least-once for async notifications/push. |
| Security | JWT RS256; OTP second factor; no client-visible driver errors; parameterized SQL only. |
| Auditability | Migrations and generated API artifacts are reviewed source. |

## 3. Key design decisions & trade-offs

| Decision | Rationale | Trade-off / ceiling |
| --- | --- | --- |
| **Domain-driven monolith** (bounded contexts, not services) | One deploy, one DB, no network hops between contexts; refactors stay in-repo | Vertical scaling limit; a hot context can't scale independently → split out later if it dominates load |
| **Strict 4-layer contexts** (`domain/service/repository/handler`) | Changes stay local; SQL swappable; testable | Boilerplate per context; enforced by review + the `new-endpoint` skill |
| **Interfaces in `domain/`, hand-written fakes** | Black-box tests without a mocking library or code-gen | Fakes are manual to maintain |
| **`GetExecutor(ctx, pool)` for all queries** | Transparent transaction awareness; a service can wrap repos in one tx | Every repo must use it — bypassing breaks tx boundaries |
| **Setter injection in `main.go`** for cross-context deps | Breaks import cycles between contexts (e.g. event→notification) | Wiring is centralized and must be kept in sync when contexts are added |
| **Nil-safe startup** | Local dev runs without full infra; prod stays strict | Handlers carry nil-guards; forgetting one turns a degraded feature into a panic |
| **RFC 7807 error contract** | Uniform, machine-readable client errors; no driver leakage | Every handler must translate at the boundary |
| **JWT RS256 + OTP (MessageCentral)** | Asymmetric keys let clients/edge verify without the signing secret; phone-first onboarding | Key management via `make keys` / k8s secrets; SMS provider dependency |
| **WS auth via `?token=` + Redis pub/sub fan-out** | Browsers can't set WS handshake headers; pub/sub lets any pod deliver to any subscriber | Token in URL needs TLS + short expiry; pub/sub adds a Redis dependency on the hot path |
| **Soft deletes (`deleted_at`)** | Recoverable data, audit trail | Every query on those tables must filter `deleted_at IS NULL` |
| **No ORM** (raw `pgx` + parameterized SQL) | Predictable queries, full control, PostGIS-friendly | More SQL to write; discipline required to keep it parameterized |

## 4. Representative flows

**OTP login**

```
client → POST /auth/otp/request {phone}
  auth.service → messagecentral.SendOTP → Redis(store code, TTL)
client → POST /auth/otp/verify {phone, code}
  auth.service → Redis(match) → user.repo(find/create) → issue JWT RS256 (access+refresh)
  ← 200 { tokens, profile }
```

**Booking → matching → messaging → notification** (cross-context, one process)

```
event.service.Create(tx)
  → matching.service.OpenLead(...)        (injected dep)
  → notification.service.Notify(pandit)   (injected dep)
  → push.service.Send(device)             (async via platform/worker)
message sent → messaging.service.Persist(tx)
  → platform/ws.Publish(conversation)     (Redis pub/sub → other pods → subscribers)
```

Background goroutines close the loop: event **auto-completion**, matching **lead expiry**, and the
**risk/trust checker** run on timers started in `main.go`.

## 5. Security model

- **Transport:** TLS terminated at Cloudflare; Traefik ingress inside the cluster.
- **AuthN:** OTP proves phone ownership; JWT RS256 access + refresh thereafter. Private signing key
  lives only in the API's environment (k8s SealedSecret); public key verifies.
- **AuthZ:** role-aware middleware; the `admin` context is a separate namespace of endpoints.
- **Data:** parameterized SQL everywhere; soft-delete filtering; no driver/SQL detail in responses.
- **Rate limiting & recovery** middleware guard abuse and panics.
- **Secrets** never enter git; `.env` locally, SealedSecrets in-cluster.

## 6. Scalability, reliability & known ceilings

- **Scaling:** stateless API pods scale horizontally; WebSocket state is externalized to Redis
  pub/sub so any pod can serve any client. **Postgres is the single writer** — the primary vertical
  ceiling. `// ceiling:` when a context (e.g. messaging or feed) dominates DB load, extract it to its
  own service and/or add read replicas before scaling the whole monolith.
- **Reliability:** transactions give strong consistency within a request; notifications/push are
  at-least-once (retryable, idempotent on the client). Startup retries transient infra failures.
- **Failure modes:**
  - DB down → prod: fatal (pod restarts); dev: features degrade.
  - Redis down → WS fan-out and rate limiting degrade; core REST continues.
  - S3 storage down → media endpoints fail; rest continues.
  - MessageCentral down → OTP requests fail (login blocked) — a hard external dependency.

## 7. Observability

Structured request logging with a per-request ID (middleware) is the baseline. **Future work:**
metrics/tracing export, and SLOs on the flows in §4.

## 8. Roadmap / open questions

- Extract the highest-load context to its own service if/when Postgres write pressure demands it.
- Add read replicas + query-level caching for feed/panchanga read paths.
- Formalize idempotency keys on push/notification delivery.
- Metrics + distributed tracing.
