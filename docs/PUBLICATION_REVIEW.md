# Public snapshot publication review

Review date: 2026-10-22. Scope: `vi-nayKR/medha-platform-api`, not the private Medha product or a live deployment.

## Boundary

This repository is a collaborative-product reference derivative. Existing contribution attribution and licensing remain unchanged; publication rights for all upstream/product material still require maintainer approval. No private product repository, credential store, database, deployment or GitHub visibility setting was changed.

## Changes in this review

- Removed the phone-specific fixed-code reviewer authentication path from both the OTP service and handler. Every normal OTP verification now delegates to MessageCentral. Regression tests exercise the formerly special identities against provider rejection and reject missing verification IDs. The separately guarded development-login path is unchanged; it must never be enabled as a production substitute.
- Bound HTTP OTP verification to the server's phone-to-challenge cache entry. A client-provided ID must match that entry; missing/empty bindings and mismatched IDs fail before the provider is called. Tests cover a different phone's challenge, substituted IDs, normal bound success, challenge consumption and a typed-nil Redis client. Verification now fails closed when that server binding is unavailable; the previous client-ID-only/cache-outage fallback is intentionally removed. The OTP cache is exposed as a small interface for hand-written test fakes without adding dependencies.
- Tightened provider verification to accept only explicit completed/success outcomes on HTTP 200. Empty/unknown outcomes and already-verified responses no longer authorize a fresh login; non-200 HTTP responses fail closed. Local HTTP-stub regression tests cover these cases without contacting MessageCentral.
- Converted account-specific seed/reset migrations **00021, 00035, 00048 and 00049** into version-preserving Up/Down no-ops. Fresh databases no longer receive developer identities, personal-contact/location fixtures or reviewer accounts through schema migration. No schema objects were removed. No rollback now deletes those accounts.
- Removed `connect_prod_db.bat`, an operational production-tunnel helper, and `database_metadata.json`, a stale duplicate schema export. Migrations remain the schema source of truth.
- Moved deployment/release workflows to `docs/workflow-reference/`. They remain historical examples, outside GitHub Actions' active workflow directory. Test/publication CI remains active; pushing this showcase must not deploy, use a self-hosted product runner, mutate the infrastructure repository or merge release tags into production.
- Corrected the README's Go requirement, nonexistent Compose Redis service and claim that blank configuration starts immediately. Added public/historical scope to architecture and agent guidance.
- Added a targeted publication guard and tests for retained account-migration versions. These are regression checks, not an exhaustive data or secret detector.

## Existing installations: action required outside this checkout

Editing an already-applied migration does **not** rerun it, remove existing accounts, revoke refresh tokens, or invalidate issued sessions. These changes must not be used as a destructive database cleanup.

Authorized product maintainers should:

1. Check whether the same reviewer bypass, unbound caller-ID verification and fail-open provider-status handling exist in the private source and deployed image; fix them there through the normal reviewed release process. This public branch does not patch a running service. Validate clients against the server-bound challenge contract before release.
2. Review reviewer/developer accounts and audit logs, then disable inappropriate access and revoke affected sessions/tokens through approved operations. Do not indiscriminately delete identities or associated user data.
3. Review any mobile-store review/access requirements separately. If a review mechanism is still needed, design a privately configured, explicitly enabled, constrained policy; do not restore an embedded phone/code bypass.
4. Create local demo accounts through normal onboarding or explicit development-only fixtures. Schema migrations are not an account-fixture distribution mechanism. Never send SMS to illustrative fixture numbers.
5. Use a disposable database to verify the full fresh/upgrade migration chain before operating an existing installation. Version/no-op tests do not substitute for that check.

## Historical exposure and scan limits

Gitleaks **8.30.1**, with default rules and full redaction, scanned all locally fetched refs: **six pre-review commits**. It returned one candidate in `internal/config/config_test.go` (a deterministic unit-test encryption-key fixture, not evidence of a live key). That fixture is now constructed as test data rather than a secret-shaped literal. No broad allowlist was added.

Manual review also found the fixed-code authentication bypass, which the generic scanner did not flag. A zero-finding generic scan alone therefore cannot establish safe authentication or synthetic provenance for personal data.

No history was rewritten. Earlier public commits still contain the removed material. The clone initially exposed only `main`, with no tags; the queried release list, issue/PR list and Actions-artifact inventory were empty. Those API observations are point-in-time, not proof that no copies, forks, caches, closed issues or past downloads exist. Closed issues and inaccessible refs were not comprehensively reviewed.

If a real live secret is subsequently identified, revoke/rotate it first. History cleanup, branch/tag deletion, cache removal or visibility changes require a separate owner-approved plan. Do not force-push as an incidental publication fix.

## Validation

Run locally:

```bash
python3 scripts/check-publication.py
go test -race ./internal/auth/... ./internal/config/... ./migrations/...
go test -race ./...
go build ./cmd/medha-api
go vet ./...
gitleaks dir --redact=100 --no-banner .
gitleaks git --redact=100 --no-banner --log-opts=--all .
```

The historical scan can continue to report old fixtures even after the working tree is clean; triage rather than silently suppressing findings. The focused PostgreSQL booking test requires `MEDHA_TEST_DATABASE_URL` and otherwise skips; no database was available during this publication review. Docker's daemon was unavailable. Full fresh/upgrade migration execution, production health, provider credentials, ownership approval and full lint are not established by unit tests or a build.

CI publication checks operate on the checked-out tree, not historical erasure. Historical workflow examples are not permission to deploy this repository.

### Recorded local results (2026-10-22)

| Command | Result |
| --- | --- |
| `go test -race ./...` | Pass, exit 0, including new auth/provider/migration regressions |
| `go build -o /tmp/medha-publication-api ./cmd/medha-api` | Pass, exit 0 |
| `go vet ./...` | Pass, exit 0 |
| `python3 scripts/check-publication.py` | Pass, exit 0 |
| `actionlint -shellcheck='' -pyflakes=''` | Pass, exit 0; optional ShellCheck/Pyflakes subprocesses disabled |
| `gitleaks dir --redact=100 --no-banner .` | Pass, exit 0, no candidates in the checked-out tree |
| `git diff --check` | Pass, exit 0 |
| `go test -race -v ./internal/interest/service -run TestConfirmBookingPostgresConcurrencyAndRollback` | Exit 0 with the database test explicitly **SKIPPED** because `MEDHA_TEST_DATABASE_URL` was unset |

Checksum-verified temporary Go 1.25.0 and Gitleaks 8.30.1 tools were used; the full golangci-lint suite and database migration/integration execution were not run. Historical findings remain as described above. These results do not verify production remediation.
