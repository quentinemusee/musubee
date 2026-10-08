# core — Go core

The Musubee core, written in Go. It runs **on the device** (a library embedded in Electron, Android and iOS) or **remotely** (hosted core).

**Status: T1.1 spike done** ([ADR 0009](../docs/ADR/0009-bridgev2-in-process.md)): `bridgev2` network connectors run in-process, without a homeserver. No domain layer or UI API yet (T1.4).

Requires **Go 1.27.1 or later**: Go 1.27.0's `database/sql` can deadlock (golang/go#81043, see ADR 0009).

## Layout

| Package | Role |
|---|---|
| `localmatrix` | Local implementation of the Matrix side of `bridgev2` (`MatrixConnector`, `MatrixAPI`): rooms, timeline, message statuses, media, stored in SQLite, with a change stream for the UI |
| `bridgehost` | Starts and stops the `bridgev2` bridges (one per network) on the shared database and the local Matrix server |
| `connector/echo` | Fake network for tests: one login flow, three contacts (instant echo, delayed echo, failed send) |
| `storage/sqlite` | Opens the SQLite database (pure-Go `modernc.org/sqlite`, no cgo) |

## Responsibilities

- **Domain**: `Account`, `Conversation`, `Message`, `Person`, and merging conversations per person. **No Matrix type leaves this layer towards the UI.**
- **Connectors**: `bridgev2` bridges from [mautrix-go](https://github.com/mautrix/go) (MPL-2.0), in "on-device" or "hosted bridge" mode.
- **Encryption**: Olm/Megolm in pure Go (`goolm` build tag, no cgo for libolm).
- **Storage**: local SQLite.
- **Local API**: commands and event streams towards the UI (contract defined in T1.4).

## Commands

Run from the repository root (`go.work` includes `core`). Verified in T1.1 on Windows with Go 1.27.1:

```
go test -count=1 ./core/...
go vet ./core/...
gofmt -l core
go test -run '^$' -bench RoundTrip -benchtime 2000x ./core/bridgehost/   (round trip and heap, see ADR 0009)
```

`go test -race` needs cgo and a 64-bit C toolchain, not available on the maintainer's Windows machine until T1.2: run it in a Linux container (`docker run --rm -v <repo>:/src -w /src golang:1.27.1 go test -race ./core/...`). CI runs it on Linux, macOS and Windows. CI also builds the core with `CGO_ENABLED=0` for Linux, Windows, macOS, Android and iOS. `golangci-lint` is not set up yet.

## Skills to re-read before coding

`go`, `golang-testing`, `golang-concurrency`, `golang-safety`, `golang-security`, `golang-error-handling`, `golang-context`, `golang-project-layout`, `golang-continuous-integration` (see [`docs/SKILLS.md`](../docs/SKILLS.md)).

## Core-specific rules

<!-- REUSE-IgnoreStart -->

- Every `.go` file starts with:
  ```go
  // SPDX-FileCopyrightText: 2026 Quentin Raimbaud
  // SPDX-License-Identifier: AGPL-3.0-or-later
  ```

<!-- REUSE-IgnoreEnd -->

- Never log message content.
- Integration tests run against a real Synapse ([`infra/`](../infra/)), never mocks.
