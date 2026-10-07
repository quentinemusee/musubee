# infra — realistic test environment

Services started with Docker for integration and end-to-end tests. Principle: **test against the real thing**, never against mocks (see [`docs/TESTING.md`](../docs/TESTING.md)). Design and measurements: [ADR 0004](../docs/ADR/0004-integration-test-environment.md).

## Contents

| Path | Purpose |
|---|---|
| `compose.test.yml` | Synapse v1.162.0 + PostgreSQL 18.6, pinned by digest |
| `synapse/` | Test-only Synapse configuration (no federation, rate limits lifted) |
| `testenv/` | Go package that starts and stops the environment and creates users; integration tests |
| `cmd/testenv/` | Command for manual work: `up`, `status`, `user`, `down` |

Requirements: Docker with the Compose plugin (Docker Desktop on Windows and macOS), Go (version in `go.mod`).

## Run the tests

From the repository root:

```
go test ./infra/...                          # unit tests, no Docker needed
go test -tags=integration ./infra/...        # integration tests: starts a fresh environment (about 12 s), then removes it
```

Without Docker the integration tests are skipped; set `MUSUBEE_REQUIRE_INTEGRATION=1` to make them fail instead (CI does).

## Use the environment by hand

```
go run ./infra/cmd/testenv up                        # prints the homeserver URL (random localhost port)
go run ./infra/cmd/testenv user alice some-password  # creates @alice:musubee.test
go run ./infra/cmd/testenv down                      # removes containers, data and secret
```

Any Matrix client can log in to the printed URL with the users you create. `-project NAME` runs several environments side by side.

## Rules

- **No secrets and no real accounts in this directory.** The registration shared secret is generated at random for every run; the developer command keeps it in `infra/.testenv/`, which Git ignores.
- Everything is throw-away: data lives in memory (tmpfs) and disappears on `down`.
- Images are pinned by digest; update tag and digest together.

## Not included yet

- **dummybridge**: blocked, its repository has no license (ADR 0004).
- Telegram bridge on the official test servers: T0.4.
- Fault-injection proxy (for example Toxiproxy): when the core needs it.
