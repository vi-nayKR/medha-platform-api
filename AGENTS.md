# AGENTS.md — Medha API

> **Cross-tool agent guide.** This file is the single source of truth for AI coding agents in
> this repository, written to the open [AGENTS.md](https://agents.md) convention.
>
> - **OpenAI Codex** (CLI / IDE / cloud) and **Google Gemini** (Gemini CLI, Antigravity, Jules)
>   read `AGENTS.md` from the repo root automatically.
> - **Anthropic Claude Code** reads `CLAUDE.md`, which is a thin pointer back to this file.
> - `GEMINI.md` is likewise a pointer. **Put guidance here — update one file, not three.**
>
> Deep dives live in [`architecture.md`](./architecture.md) (as-built structure) and
> [`system_design.md`](./system_design.md) (design decisions, data flows, quality attributes).

---

## 1. What this is

Backend API for the **Medha** platform — a marketplace connecting **Yajmans** (clients who book
spiritual ceremonies) with **Pandits** (priests). Written in **Go 1.25**
(`module github.com/medha/backend`). It is a **domain-driven monolith** exposing a V2 REST API:
OTP-based auth, user/pandit profiles, events, matching, interests, messaging, notifications,
push, panchanga, social feed, and an admin namespace.

Deployed as a container into `dev`/`prod` namespaces of a self-hosted Kubernetes cluster owned by
[`medha-infra`](../medha-infra). Consumed by [`medha-android`](../medha-android) and
[`medha-ios`](../medha-ios) (clients), [`medha-admin-panel`](../medha-admin-panel) (operators), and
[`medha-web`](../medha-web) (public release and Panchanga data).

**Delivery status (2026-07-22):** the Medha system is complete end to end in dev and production.
Both Kubernetes overlays currently pin this repository's `db5827f` image. Treat new work as
maintenance or incremental product improvement; the only known active client-side polish is in
`medha-ios`.

## 2. Setup & common commands

Use the `Makefile` targets — they wire up `$GOPATH/bin` tools and load `.env`.

| Task | Command |
| --- | --- |
| Build binary → `bin/medha-api` | `make build` |
| Run (build + exec) | `make run` |
| Hot reload (Air) | `make dev` |
| Full local dev (sync env + Air) | `make start` |
| Tests (race, verbose) | `make test` → `go test -race -v ./...` |
| Single test | `go test -race -run TestName ./internal/<domain>/...` |
| Lint / autofix | `make lint` / `make lint-fix` (golangci-lint) |
| Migrate up / down / status | `make migrate-up` / `migrate-down` / `migrate-status` (goose) |
| New migration | `make migrate-create name=<name>` |
| Regenerate OpenAPI + Bruno/Postman | `make swagger` |
| Generate dev RS256 JWT keys | `make keys` |
| Local Postgres + Redis (Docker) | `make services-up` / `services-down` / `services-reset` |

Migrations can also run at startup via the binary flag: `medha-api -migrate`.
First-time setup: copy `.env.example` → `.env`, run `make keys`, then `make services-up`.

## 3. Project structure

```
cmd/
  medha-api/        # main executable entry point (cmd/medha-api/main.go)
  deploy/ genkeys/ sync-bruno/ sync-postman/ utils/   # generators & ops tools
internal/
  <domain>/         # bounded contexts (see below), layered domain/repository/service/handler
  infra/            # database (pgxpool + GetExecutor), cache (Redis), storage (S3-compatible)
  platform/         # ws (WebSocket hub), worker (async pool), messagecentral (OTP SMS), epoch
  server/           # chi router (server.go) + middleware/ (auth, cors, logging, rate_limit, ...)
  config/           # env loading; cfg.IsDevelopment() / IsProduction()
pkg/                # crypto, response (wrapped envelopes), errors (RFC 7807), image
migrations/         # goose SQL migrations (~49)
api/openapi/        # OpenAPI spec + generated docs
```

**Bounded contexts** (`internal/<domain>/`): `auth`, `user` (incl. pandit), `event`, `matching`,
`interest`, `messaging`, `notification`, `push`, `social`, `story`, `panchanga`, `city`,
`location`, `storage`, `festivallogos`, `apprelease`, `systempublisher`, `admin`.

Each context follows **strict layering** (never skip a layer, never reach across):

```
internal/<domain>/
  domain/       # entities, sentinel errors (ErrXxx), repository interfaces
  repository/   # postgres_*.go — SQL only, implements domain interfaces
  service/      # business workflows, orchestrates repositories
  handler/      # thin HTTP: decode → validate → call service → respond
```

Cross-context wiring uses **setter injection in `main.go`** (e.g.
`eventSvc.SetNotificationDependencies(...)`) to avoid import cycles.

## 4. Coding conventions (enforced)

- **Context first:** `ctx context.Context` is the first arg to every repo/service/client function.
- **Errors:** never discard with `_`. Define `Err`-prefixed sentinel errors at the domain layer;
  wrap with `fmt.Errorf("...: %w", err)`. Translate to RFC 7807 Problem Details at the handler
  boundary via `pkg/errors` — **never leak SQL/driver detail to clients**. No `panic`/`recover` in
  business logic.
- **SQL safety:** always parameterized (`$1`, `$2`), **never** string-concatenated. Route through
  `database.GetExecutor(ctx, pool)` so queries are transaction-aware. Add `deleted_at IS NULL` on
  soft-deletable tables.
- **Layer separation:** handlers thin, services own logic, repositories own SQL.
- **Validation:** validate DTOs at the handler layer with `go-playground/validator/v10`
  (`validate.Struct()`) before calling services.
- **JSON:** struct fields use `json:"snake_case"`.
- **Formatting:** `gofmt` + `goimports` with local-prefix `github.com/medha/backend`. Lint with
  golangci-lint (govet, staticcheck, ineffassign, bodyclose, nilerr, copyloopvar).

## 5. Testing

- Go's standard `testing` package; test files use the **external `_test` package** (black-box).
- **No mocking library** — hand-write fakes that implement the domain repository interfaces in
  `*_test.go` (see `internal/interest/service/interest_service_test.go`).
- Run `make test` and `make lint` before submitting. CI runs `go test -race ./...`.

## 6. Git workflow (GitFlow-lite)

`feature/*` → PR into `dev` → PR into `main` → tag `vX.Y.0` (patch must be `0`) auto-merges to
`prod` → auto-deploy. **No direct commits to `main` or `prod`.** Squash-and-merge into `dev`.
Conventional Commit subjects (`feat:`, `fix:`, `docs:`, `chore:`, `security:`). PRs must include
testing evidence and migration/API details; CI, race tests, ShellCheck, and CodeRabbit review must
pass before merge.

## 7. Security & configuration

- Copy `.env.example` → `.env`; **never commit** secrets, JWT keys, or credentials.
- `make keys` generates local RS256 keys. Auth is JWT RS256; V2 login is OTP via MessageCentral.
- **WebSocket auth gotcha:** browsers can't set headers on the WS handshake, so the JWT arrives as a
  `?token=` query param and is validated in `internal/platform/ws/handler.go` — never assume an
  `Authorization` header there.
- Treat migrations and generated API artifacts (`api/openapi/`, Bruno, Postman) as reviewable source.

## 8. Agent guardrails — do / don't

- **Do** add a new endpoint through the full stack (domain → repository → service → handler → route)
  and a black-box `_test.go`. The `.claude/skills/new-endpoint` skill encodes the exact recipe.
- **Do** keep startup **nil-safe**: in development, missing DB/Redis/S3 storage degrade gracefully; in
  production they are a fatal exit. Don't remove nil-guards in handlers.
- **Don't** introduce a mocking library, an ORM, or string-built SQL.
- **Don't** widen a handler into business logic or put SQL in a service.
- **When unsure** about product/architecture context, see [`architecture.md`](./architecture.md),
  [`system_design.md`](./system_design.md), and `README.md` (branch strategy, admin endpoints).
