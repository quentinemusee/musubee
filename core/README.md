# core — Go core

The Musubee core, written in Go. It runs **on the device** (a library embedded in Electron, Android and iOS) or **remotely** (hosted core).

**Status: T1.1 to T1.8, T2.1 and T2.2 done.** `bridgev2` network connectors run in-process, without a homeserver ([ADR 0009](../docs/ADR/0009-bridgev2-in-process.md)), the core builds as a C shared library driven from a C program ([ADR 0010](../docs/ADR/0010-core-shared-library.md)), and the same library runs in the Android app through JNI ([ADR 0011](../docs/ADR/0011-core-on-android.md)). The UI talks to it through a versioned contract: a JSON Schema with generated Go and TypeScript types ([ADR 0012](../docs/ADR/0012-core-api-contract.md)). On the desktop, the core runs as its own program over stdio, supervised by the Electron app ([ADR 0013](../docs/ADR/0013-desktop-shell.md)). The first real network, Telegram, runs on the device with mautrix-telegram's connector ([ADR 0014](../docs/ADR/0014-telegram-on-device.md)). The user's own Matrix account is a network too: the core is an end-to-end encrypted Matrix client of its homeserver ([ADR 0018](../docs/ADR/0018-native-matrix-accounts.md)).

Requires **Go 1.27.1 or later**: Go 1.27.0's `database/sql` can deadlock (golang/go#81043, see ADR 0009).

## Layout

| Package | Role |
|---|---|
| `localmatrix` | Local implementation of the Matrix side of `bridgev2` (`MatrixConnector`, `MatrixAPI`): rooms, timeline, message statuses, media, stored in SQLite, with a change stream for the UI |
| `bridgehost` | Starts and stops the `bridgev2` bridges (one per network) on the shared database and the local Matrix server |
| `connector/echo` | Fake network for tests: two login flows (a username, or a code to display and wait for), three contacts (instant echo, delayed echo, failed send) |
| `connector/matrix` | The user's own Matrix account: the core signs in to the homeserver, syncs, mirrors the joined rooms as conversations and sends text, with end-to-end encryption (mautrix-go's `cryptohelper` on goolm) and its crypto and state stores in the core's database (ADR 0018) |
| `connector/telegram` | Telegram on the device: mautrix-telegram's connector with our defaults (credentials, device name, no external sticker converters, no "manual" login flow). Offered only when the core is given the application credentials (ADR 0014) |
| `replace/webp` | Pure-Go stand-in for `go.mau.fi/webp` (libwebp through cgo), used through a `replace` directive: PNG and JPEG stickers cannot be sent to Telegram (ADR 0014) |
| `storage/sqlite` | Opens the SQLite database: pure-Go `modernc.org/sqlite` (no cgo), except on Android and with the `musubee_cgo_sqlite` tag, which use `mattn/go-sqlite3` (cgo; ADR 0011) |
| `api` | The core API contract: the JSON Schema (`schema/`), shared test examples, and the Go types generated from it (`types.gen.go`, by `go generate ./core/api`) |
| `api/apitest` | Validates JSON documents against the schema, and finds Matrix identifiers in them, for the contract tests (test code only) |
| `api/internal/codegen`, `api/apigen` | The generator of the Go and TypeScript types (`ui/src/core-api/types.gen.ts`) |
| `api/stream` | Serves the API as newline-delimited JSON on a reader and a writer: requests run concurrently, events are interleaved, `core.closed` is the last line |
| `api/transportbench` | Serves a core over stdio or loopback TCP, for the transport measurements of ADR 0012 (`scripts/transportbench`) |
| `cmd/musubee-core` | The core as a program (`-data DIR`, `-log-level`; Telegram's application credentials in `MUSUBEE_TG_API_ID` and `MUSUBEE_TG_API_HASH`), speaking `api/stream` on its standard input and output; it exits when its input ends. The desktop app's child process |
| `embedded` | The core as one object for an embedding app: implements the API commands and the event stream on top of `bridgehost`, `localmatrix` and `persons`; no Matrix type leaves it (its tests check every response and event) |
| `persons` | The persons the user merged conversations into, and their links to conversations, keyed by each conversation's identity on its network, in `musubee_` tables of the core's database (ADR 0017) |
| `ffi` | The C shared library (`musubee.h`) around `embedded`; with the SQLite driver of Android builds, the only code that needs cgo. On Android it also holds the JNI entry points of the app (`jni_android.go`). `testdata/host.c` is the C host program of its tests |

## Responsibilities

- **Domain**: `Account`, `Conversation`, `Message`, `Person`, and merging conversations per person. **No Matrix type leaves this layer towards the UI.**
- **Connectors**: `bridgev2` bridges from [mautrix-go](https://github.com/mautrix/go) (MPL-2.0), in "on-device" or "hosted bridge" mode.
- **Encryption**: Olm/Megolm in pure Go (`goolm` build tag, no cgo for libolm).
- **Storage**: local SQLite.
- **Local API**: commands and event streams towards the UI, defined by `api/schema/core-api.schema.json` (ADR 0012).

## Commands

Run from the repository root (`go.work` includes `core`). Verified in T1.1 and T1.2 on Windows with Go 1.27.1. Since T2.2, **every Go build of the core needs the `goolm` build tag**: without it, mautrix-go's crypto package links libolm through cgo and fails on `olm/olm.h` (ADR 0018).

```
go test -tags=goolm -count=1 ./core/...
go vet -tags=goolm ./core/...
gofmt -l core
go test -tags=goolm -run '^$' -bench RoundTrip -benchtime 2000x ./core/bridgehost/   (round trip and heap, see ADR 0009)
go test -tags=goolm -race -count=1 ./core/...
go build -tags=goolm -buildmode=c-shared -o musubee.dll ./core/ffi                    (libmusubee.so on Linux)
go test -tags=goolm -count=1 -v -run TestSharedLibrary ./core/ffi/                    (FFI round trip, size and memory, see ADR 0010)
go generate ./core/api                                                    (Go and TypeScript types from the schema)
go test -tags=goolm -run '^$' -bench RoundTrip -benchtime 2000x ./core/embedded/      (round trip through the API, see ADR 0012)
go build -tags=goolm -o musubee-core.exe ./core/cmd/musubee-core                      (the desktop app's child process)
golangci-lint run ./core/... ./infra/... ./scripts/licenseaudit/...      (configuration: .golangci.yml)
go test -tags=goolm,integration -count=1 -v ./infra/matrixtest/       (Matrix accounts against Synapse, needs Docker; ADR 0018)
go build -tags=goolm -trimpath -ldflags="-s -w" -o musubee-core.exe ./core/cmd/musubee-core   (stripped, as measured in ADR 0014)
go test -tags=goolm,telegram -v ./infra/telegramtest/ondevice/            (Telegram on the device with the test bots, see infra/README.md)
```

The `replace go.mau.fi/webp => ./replace/webp` directive of `go.mod` is not inherited by modules that import the core: `infra/go.mod` repeats it, and any other module must too.

`core/ffi` and `go test -race` need cgo and a 64-bit C toolchain: on Windows, MSYS2 UCRT64 GCC first in `PATH` (`CLAUDE.md` §8). Without one, `CGO_ENABLED=0 go test -tags=goolm ./core/...` runs everything else; a Linux container also works (`docker run --rm -v <repo>:/src -w /src golang:1.27.1 go test -tags=goolm -race ./core/...`). CI runs the race detector and the FFI test on Linux, macOS and Windows. CI also builds the core with `CGO_ENABLED=0` for Linux, Windows, macOS and iOS, and runs the whole suite a second time with the SQLite driver of Android builds (`-tags=goolm,musubee_cgo_sqlite`, needs cgo). CI runs `golangci-lint` 2.14 (`.golangci.yml`: the standard linters plus gosec, errorlint, noctx and a few others), also with the `musubee_cgo_sqlite` tag.

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
