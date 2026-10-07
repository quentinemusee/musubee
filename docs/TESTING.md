# Testing strategy — Musubee

Principle: **test against the real thing**. Setting up the environment may take a long time; that is acceptable. A test that passes against a mock proves nothing about Matrix, the bridges or iOS.

## Levels

| Level | Tools | What it proves | When |
|---|---|---|---|
| Go unit | `go test -race`, fuzzing on parsers, `goleak` for goroutine leaks | Domain logic, conversation merging, conversions | Every commit |
| UI unit | Vitest + Testing Library | Components, states, themes | Every commit |
| Integration | Docker compose: **Synapse + Postgres** + real connectors; Go tests tagged `integration` | The core really speaks Matrix and talks to the bridges | Every PR (Linux runner) |
| Contract | Versioned core ↔ UI API schema | UI and core stay compatible | Every PR |
| Desktop E2E | Playwright (supports Electron) | Complete user journeys on Windows/macOS/Linux | Every PR (runners for the 3 OSes) |
| Android E2E | Android emulator (KVM on Linux) + UI tool (Maestro or Appium, decided in T0.5) + **real device** before each release | Foreground service, notifications, network recovery | PR + release |
| iOS E2E | Xcode simulator (macOS runner) + **real iPhone** | Notifications, NSE, background, memory | Release (the simulator does not replace the device for background behavior) |

## Installed testing skills
`playwright-testing`, `playwright-cli` and `webapp-testing` (UI and Electron E2E); `capacitor-testing` (mobile unit, E2E and native tests); `swift-testing`, `ios-simulator` (iOS); `golang-testing` (core, with `goleak`, fuzzing, parallel tests). Re-read the skill for the relevant level before writing a test.

## Realistic environment (`infra/compose.test.yml`)

In place since T0.3 ([ADR 0004](ADR/0004-integration-test-environment.md), usage in [`infra/README.md`](../infra/README.md)): `go test -tags=integration ./infra/...` starts a fresh environment, runs the tests and removes it.

- **Synapse** (reference homeserver) + **PostgreSQL**: in place.
- **Echo connector** (ours, written in T1.1): a `bridgev2` connector that simulates a network (fake contacts, echoed messages, failure modes) to test the whole bridge pipeline without an external network. It replaces Beeper's dummybridge, whose repository has no license (ADR 0004).
- **Telegram — official test servers**: an environment parallel to production. Reserved numbers `99966XYYYY` (X = datacenter 1 to 3); the login code is X repeated 5 or 6 times; these numbers only work on the test DCs. Usable in CI (no real user account).
  - *Credentials (verified in the official docs on 2026-10-07):* any Telegram client, including one talking to the test DCs, needs an `api_id` / `api_hash` pair. It is issued at my.telegram.org after signing in with a real phone number (<https://core.telegram.org/api/obtaining_api_id>), and the test DC addresses are only shown in that panel (<https://core.telegram.org/api/auth>). The pair identifies the *application*, not a user: CI never signs in to the maintainer's account, only to `99966XYYYY` test accounts. It is stored as an encrypted GitHub Actions secret, as allowed by rule 3 of `CLAUDE.md` §6. Whether the sample `api_id` shipped with Telegram's open-source code can be used instead for testing is **unknown**, to check in T0.4.
- **Signal / WhatsApp**: as far as we know, there is no equivalent public sandbox for WhatsApp (to verify in T0.4). Dedicated accounts, low volume, **never in CI**, documented manual tests. Expect a risk of account bans.
- **Network faults**: a fault-injection proxy (e.g. Toxiproxy) between the core and the servers to simulate outages, latency and packet loss.

## The tests that matter most for this project

1. **Confidentiality test**: after sending a message with encryption enabled, query the Synapse database and **verify that no plaintext is present**. Must fail the build if it happens.
2. **Multi-network end-to-end**: a message sent from a Telegram test client → arrives in Musubee → reply → received on the Telegram side.
3. **Conversation merging**: two conversations from different networks linked to the same person are shown as one thread, the order is stable, splitting them back is possible, no message is lost.
4. **Recovery**: kill the process and cut the network while sending; no duplicates, no loss.
5. **iOS NSE memory budget**: test on a real iPhone, documented threshold (see T1.7). Exceeding it fails the test.
6. **Media**: voice messages, photos, GIFs, large files: sending, receiving, encryption, resumed upload.
7. **Upgrade / migration** of the local database between versions.

## Quality and compliance in CI

- `go vet`, `golangci-lint`, `go test -race ./...`
- `npm run typecheck`, lint, tests
- **Repository checks** (in place since T0.1), workflow `.github/workflows/checks.yml` on Linux and Windows:
  - SPDX headers: `python scripts/license_check.py` runs `reuse lint` (REUSE 3.3 specification). See `docs/ADR/0002-license-and-reuse.md`.
  - English only: `python scripts/language_check.py` rejects French text in tracked files.
  - Install scripts: the `.ps1`, `.bat` and `.sh` variants are run in dry-run mode and must print the same commands.
  - Tests of these checks: `python -m unittest discover -s scripts/tests -v`.
- **Dependency license audit** (Go and npm, T0.6): fails on any dependency incompatible with AGPL-3.0
- Secret scanning
- Reproducible builds as far as possible; signed artifacts for releases

## Required hardware

- A **Mac** (or macOS runner) for iOS; a **paid Apple Developer account** for push and the NSE on a real device (a free account can install on your own iPhone, expiring after 7 days, but without push capabilities).
- A real Android phone and a real iPhone.

## Definition of "done" for a task

1. Tests written and run, results pasted into the PR.
2. No regression in the full suite.
3. Licenses checked, `NOTICE` up to date.
4. Documentation / ADR updated if a decision changed.
5. Explicit distinction between what is **verified**, **assumed** and **unknown**.
