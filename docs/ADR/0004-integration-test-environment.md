# ADR 0004 — Integration test environment: Synapse and PostgreSQL with Docker Compose

- **Status**: accepted
- **Date**: 2026-10-07
- **Task**: T0.3
- **Deciders**: Quentin Raimbaud (maintainer), with the design assistant

## Context

`CLAUDE.md` requires integration tests against a real homeserver, never mocks. T0.3 asks for `infra/compose.test.yml` with Synapse, PostgreSQL and Beeper's dummybridge, start and stop tooling, and a test in which a Matrix client creates a room, sends a message and reads it back. The environment must run on the maintainer's Windows machine (Docker Desktop) and in CI.

## Options considered

### Lifecycle tooling
- **Shell scripts in three variants** (`.ps1`, `.bat`, `.sh`), as for the skill installers: three implementations to keep in sync, and the tests would still need their own Go code to drive Compose.
- **A Go package plus a small Go command (chosen)**: `infra/testenv` starts, inspects and stops the environment; the integration tests and the developer command `go run ./infra/cmd/testenv` share it. Go already runs on every developer platform and in CI.

### Data and secrets
- Named volumes and a fixed shared secret in the repository: simple, but a secret in the repository (even a test one) breaks rule 8, and leftovers survive crashed runs.
- **Memory-backed storage and a per-run random secret (chosen)**: PostgreSQL and Synapse data live in tmpfs; the registration shared secret is generated for every run and mounted read-only; Synapse generates its signing key at startup.

### Test bridge
- **dummybridge (beeper/dummybridge)**, the bridge named in `docs/TASKS.md`: a `bridgev2` connector generating fake data, published as `ghcr.io/beeper/dummybridge`.
- A bridge of our own, written with `bridgev2`.
- No bridge in T0.3.

## Decision

1. `infra/compose.test.yml` runs **Synapse v1.162.0** (`ghcr.io/element-hq/synapse`, AGPL-3.0) and **PostgreSQL 18.6** (`postgres:18.6-alpine`, PostgreSQL License), both **pinned by digest**.
2. Synapse is configured for tests only (`infra/synapse/homeserver.yaml`): server name `musubee.test`, client API only, no federation, no outbound key servers, presence and URL previews off, rate limits lifted, registration closed except through the shared-secret admin API. PostgreSQL uses `trust` authentication and publishes no port.
3. Only Synapse's client API is published, on a **random port bound to 127.0.0.1**. Each environment uses its own Compose project name, so several can run side by side.
4. `infra/testenv` (Go module `github.com/quentinemusee/musubee/infra`, in a `go.work` workspace at the repository root) starts the stack with `docker compose up --wait`, creates users through the shared-secret API (HMAC-SHA1, as Synapse requires), and logs in with **mautrix-go v0.31.0**. `Stop` removes containers, networks, volumes and the secrets directory.
5. Integration tests carry the `integration` build tag (`go test -tags=integration ./infra/...`), following the `golang-testing` skill. Without Docker they are skipped, unless `MUSUBEE_REQUIRE_INTEGRATION=1`, which CI sets so that a missing Docker fails the build.
6. **dummybridge is not used for now: its repository has no license** ("This project currently does not have a published license", README, read on 2026-10-07). Without a license we have no written right to run or reuse it, and T1.1 planned to build on its connector code. See "Consequences".

## Measurements

| Measure | Value | Conditions |
|---|---|---|
| Time until both services are healthy | 11 to 12 s | Windows 11, Docker Desktop, images already pulled |
| Whole integration test run | ≈ 19 s | same, `go test -tags=integration -count=1 ./infra/...` |

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| Synapse v1.162.0 is the latest release (2026-09-29) and is licensed AGPL-3.0 | **verified** | GitHub API, element-hq/synapse, 2026-10-07 |
| Synapse generates a missing signing key at startup | **verified** (startup with an empty `/data`) | local run |
| Shared-secret registration: GET a nonce, then POST with HMAC-SHA1 over nonce, user, password and `admin`/`notadmin`, separated by NUL bytes | **verified** (official docs; Go implementation checked against vectors computed with Python's `hmac`) | <https://element-hq.github.io/synapse/latest/admin_api/register_api.html> |
| A wrong shared secret is rejected with HTTP 403 | **verified** (integration test) | `TestRegistrationRejectsAWrongSharedSecret` |
| A client creates a room, sends a message and reads it back; a second user joins and reads it | **verified** (integration test, real Synapse) | `TestRoomMessageRoundTrip` |
| The environment leaves no container, network or secret behind | **verified** (checked after test and CLI runs) | local run |
| mautrix-go's latest release is v0.31.0 (2026-09-16), MPL-2.0 | **verified** | GitHub API, mautrix/go |
| Compiled dependencies are MPL-2.0 (mautrix-go, go.mau.fi/util), MIT (zerolog, tidwall/*, mattn/*) or BSD-3-Clause (golang.org/x/*, filippo.io/edwards25519), all compatible with AGPL-3.0-or-later | **verified** (license files in the module cache) | `go list -deps`, 2026-10-07 |
| beeper/dummybridge has no license | **verified** (README "License" section, GitHub API) | <https://github.com/beeper/dummybridge> |
| The integration job works on GitHub's Ubuntu runners | **verified**: environment ready in 29 s, all integration tests pass (after a permission fix for Linux bind mounts) | CI run 37623051337 |
| `go test -race` works on the maintainer's Windows machine | **verified not to work** today: the only `gcc` is 32-bit MinGW.org ("64-bit mode not compiled in"); the race detector needs cgo with a 64-bit C toolchain. Race tests run in CI and in a Linux container. To be fixed with the Windows C toolchain in T1.2. | local run |

## Consequences

- Anyone with Docker gets a disposable Matrix homeserver in about 12 seconds, from the tests or with `go run ./infra/cmd/testenv up`.
- On Windows, the per-run secret of the developer command lives in `infra/.testenv/` (ignored by Git) rather than in `AppData\Local`, because packaged (MSIX) applications see a redirected `AppData\Local` that Docker Desktop cannot read.
- **dummybridge is blocked by licensing.** Options considered: (1) ask Beeper to publish a license; (2) write our own minimal echo connector with `bridgev2` (AGPL), which T1.1 needs anyway to prove in-process bridging; (3) use dummybridge as a black-box image in tests only, accepting the legal uncertainty. **Decision (maintainer, 2026-10-07): option 2**, built in T1.1. Its code (about 2,100 lines, much of it Beeper-specific) is not read or copied.
- Integration tests run on Linux runners only: GitHub's Windows and macOS runners cannot run these Linux containers. Windows developers run them locally with Docker Desktop.

## Sources

- Synapse configuration manual: <https://element-hq.github.io/synapse/latest/usage/configuration/config_documentation.html>; shared-secret registration: <https://element-hq.github.io/synapse/latest/admin_api/register_api.html> (accessed 2026-10-07).
- mautrix-go v0.31.0 source (`client.go`, `requests.go`, `versions.go`), read in the Go module cache on 2026-10-07.
- beeper/dummybridge README, accessed 2026-10-07.
- Docker Hub `library/postgres` tags and GitHub Container Registry `element-hq/synapse`, accessed 2026-10-07.
