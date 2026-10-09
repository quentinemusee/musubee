# core — Go core

The Musubee core, written in Go. It runs **on the device** (a library embedded in Electron, Android and iOS) or **remotely** (hosted core).

**Status: T1.1 to T1.4 done.** `bridgev2` network connectors run in-process, without a homeserver ([ADR 0009](../docs/ADR/0009-bridgev2-in-process.md)), the core builds as a C shared library driven from a C program ([ADR 0010](../docs/ADR/0010-core-shared-library.md)), and the same library runs in the Android app through JNI ([ADR 0011](../docs/ADR/0011-core-on-android.md)). The UI talks to it through a versioned contract: a JSON Schema with generated Go and TypeScript types ([ADR 0012](../docs/ADR/0012-core-api-contract.md)).

Requires **Go 1.27.1 or later**: Go 1.27.0's `database/sql` can deadlock (golang/go#81043, see ADR 0009).

## Layout

| Package | Role |
|---|---|
| `localmatrix` | Local implementation of the Matrix side of `bridgev2` (`MatrixConnector`, `MatrixAPI`): rooms, timeline, message statuses, media, stored in SQLite, with a change stream for the UI |
| `bridgehost` | Starts and stops the `bridgev2` bridges (one per network) on the shared database and the local Matrix server |
| `connector/echo` | Fake network for tests: two login flows (a username, or a code to display and wait for), three contacts (instant echo, delayed echo, failed send) |
| `storage/sqlite` | Opens the SQLite database: pure-Go `modernc.org/sqlite` (no cgo), except on Android and with the `musubee_cgo_sqlite` tag, which use `mattn/go-sqlite3` (cgo; ADR 0011) |
| `api` | The core API contract: the JSON Schema (`schema/`), shared test examples, and the Go types generated from it (`types.gen.go`, by `go generate ./core/api`) |
| `api/apitest` | Validates JSON documents against the schema, for the contract tests (test code only) |
| `api/internal/codegen`, `api/apigen` | The generator of the Go and TypeScript types (`ui/src/core-api/types.gen.ts`) |
| `api/transportbench` | Serves a core over stdio or loopback TCP, for the transport measurements of ADR 0012 (`scripts/transportbench`) |
| `embedded` | The core as one object for an embedding app: implements the API commands and the event stream on top of `bridgehost` and `localmatrix`; no Matrix type leaves it |
| `ffi` | The C shared library (`musubee.h`) around `embedded`; with the SQLite driver of Android builds, the only code that needs cgo. On Android it also holds the JNI entry points of the app (`jni_android.go`). `testdata/host.c` is the C host program of its tests |

## Responsibilities

- **Domain**: `Account`, `Conversation`, `Message`, `Person`, and merging conversations per person. **No Matrix type leaves this layer towards the UI.**
- **Connectors**: `bridgev2` bridges from [mautrix-go](https://github.com/mautrix/go) (MPL-2.0), in "on-device" or "hosted bridge" mode.
- **Encryption**: Olm/Megolm in pure Go (`goolm` build tag, no cgo for libolm).
- **Storage**: local SQLite.
- **Local API**: commands and event streams towards the UI, defined by `api/schema/core-api.schema.json` (ADR 0012).

## Commands

Run from the repository root (`go.work` includes `core`). Verified in T1.1 and T1.2 on Windows with Go 1.27.1:

```
go test -count=1 ./core/...
go vet ./core/...
gofmt -l core
go test -run '^$' -bench RoundTrip -benchtime 2000x ./core/bridgehost/   (round trip and heap, see ADR 0009)
go test -race -count=1 ./core/...
go build -buildmode=c-shared -o musubee.dll ./core/ffi                    (libmusubee.so on Linux)
go test -count=1 -v -run TestSharedLibrary ./core/ffi/                    (FFI round trip, size and memory, see ADR 0010)
go generate ./core/api                                                    (Go and TypeScript types from the schema)
go test -run '^$' -bench RoundTrip -benchtime 2000x ./core/embedded/      (round trip through the API, see ADR 0012)
```

`core/ffi` and `go test -race` need cgo and a 64-bit C toolchain: on Windows, MSYS2 UCRT64 GCC first in `PATH` (`CLAUDE.md` §8). Without one, `CGO_ENABLED=0 go test ./core/...` runs everything else; a Linux container also works (`docker run --rm -v <repo>:/src -w /src golang:1.27.1 go test -race ./core/...`). CI runs the race detector and the FFI test on Linux, macOS and Windows. CI also builds the core with `CGO_ENABLED=0` for Linux, Windows, macOS and iOS, and runs the whole suite a second time with the SQLite driver of Android builds (`-tags=musubee_cgo_sqlite`, needs cgo). `golangci-lint` is not set up yet.

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
