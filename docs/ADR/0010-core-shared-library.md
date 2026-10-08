# ADR 0010 — The core as a C shared library (Windows and Linux)

- **Status**: accepted
- **Date**: 2026-10-08
- **Task**: T1.2
- **Deciders**: Quentin Raimbaud (maintainer), with the design assistant

## Context

On the device, the Go core is a library inside the app: Electron on the desktop, a Capacitor plugin on mobile (`CLAUDE.md` §2). Open question 2 of `CLAUDE.md` §4 asks whether it builds cleanly as a shared library, at what size and memory cost, and with which C toolchain. T1.2 answers it for Windows and Linux; Android is T1.3, iOS later. ADR 0001 also left a question for T1.2: SQLite through cgo (`mattn/go-sqlite3`) or in pure Go (`modernc.org/sqlite`, chosen provisionally in ADR 0009).

How the UI talks to the core (direct FFI or a local socket, open question 4) is **not** decided here: T1.4 compares them. T1.2 only needs a C interface good enough to drive the core from another language and measure it.

## Options considered

### Interface shape

- **A. Callbacks**: the host registers a C function that the core calls for each event. gomuks' FFI package (`pkg/ffi/ffi.go`, MPL-2.0, read for inspiration; no code copied) works this way, with handles and buffers owned by one side. Cons: the callback runs on a Go-created thread. Node.js (N-API), the JVM (JNI `AttachCurrentThread`) and Swift each need extra code to receive calls on a foreign thread, and a slow callback blocks the core.
- **B. Pull (chosen)**: the host calls a blocking `musubee_next_event` from a thread it owns. No call ever goes from Go into the host. Backpressure is explicit: the core queues up to 1,024 events. When the host reads too slowly, the oldest events are dropped and an `overflow` event tells the host to re-read the state, which stays the source of truth (ADR 0009, point 1). Cons: the host dedicates a thread to reading events. Every target already has one available (a worker thread in Electron's main process, a coroutine in Kotlin, a task in Swift).

### Data format

JSON in UTF-8 for requests, responses and events: one stable C signature for every command, so new commands need no new C functions, and every host language has a JSON parser. The cost is measured below (ping). The versioned schema and the final format are T1.4.

### SQLite driver

- **modernc.org/sqlite** (BSD-3-Clause; SQLite transpiled to Go): no cgo, so the core outside `core/ffi` builds for every target with `CGO_ENABLED=0`.
- **mattn/go-sqlite3** (MIT; the SQLite C amalgamation through cgo): the reference C SQLite and a slightly smaller binary, but a C toolchain for every target, even for unit tests.

Both were measured with the same core, behind a temporary build tag (Measurements). The variant was removed afterwards.

## Decision

1. **Go: the core builds as a C shared library** with `go build -buildmode=c-shared ./core/ffi`: `musubee.dll`, `libmusubee.so` (and `libmusubee.dylib`, exercised in CI on macOS). The interface is `core/ffi/musubee.h`, five functions:
   - `musubee_open(config, error)` returns a handle (0 on failure, with a message);
   - `musubee_call(handle, request)` runs one JSON request and returns its JSON response;
   - `musubee_next_event(handle, timeout_ms)` returns the next JSON event, or an empty buffer on timeout;
   - `musubee_close(handle)`;
   - `musubee_free(buffer)`.
2. **Ownership**: every buffer the library returns is allocated with `malloc` and released by the host with `musubee_free`. The library copies its inputs and keeps no pointer to host memory.
3. **Safety**:
   - Every exported function recovers Go panics and turns them into an error response: a panic crossing into C would abort the host process without a trace.
   - Handles are keys in a map, not `cgo.Handle`, so a stale or invalid handle returns an error (`invalid or closed handle`, or a `closed` event) instead of crashing.
   - The C compiler checks the prototypes of `musubee.h` against the declarations cgo generates. cgo cannot express `const`, so the exports take a `typedef const uint8_t musubee_const_byte` from the preamble, and a mismatch is a compile error ("conflicting types": checked by mutation).
4. **The library is never unloaded.** Go does not support `dlclose`/`FreeLibrary` of a c-shared library (golang/go#11100, open). The core can be closed and reopened within one process (`TestReopenKeepsTheLogin`), but the Go runtime stays until the process exits.
5. **cgo stays confined to `core/ffi`.** Its files require cgo (a build constraint), so with `CGO_ENABLED=0` the package has nothing to build and `./core/...` still cross-builds for every target. The Go-side surface, `core/embedded`, is plain Go and tested without cgo: configuration, commands (`ping`, `login`, `rooms`, `send`, `stats`), event stream, close.
6. **Windows C toolchain: MSYS2, UCRT64 environment** (GCC, maintained, links against the Universal C Runtime that ships with Windows 10 and later). The library imports only `KERNEL32.dll` and the UCRT (`api-ms-win-crt-*`): no MinGW runtime DLL to ship. Setup in `CLAUDE.md` §8.
7. **SQLite: keep modernc.org/sqlite.**
   - It is as fast as mattn on Linux, and **5 times faster on Windows** in our round trip. The mattn slowdown comes from SQLite's busy handler, which sleeps through the Windows timer: about 15.6 ms by default. With `timeBeginPeriod(1)` in the host, mattn matches modernc; modernc sleeps through the Go runtime's timers. A library must not change the timer resolution of the whole process.
   - The cost is 2.3 to 2.6 MB more per stripped library.
   - It keeps unit tests and cross-builds free of cgo.
   - The measurement also shows that one writer waits for the lock during each round trip: a point to watch when the storage design is settled.
8. **Shutdown is not graceful in bridgev2.** `Bridge.Stop` disconnects the logins, then cancels the context of the portal event handlers without waiting for them. Handlers still running fail with `context canceled`, and log after `Stop` has returned. Same on mautrix-go `main` (2026-10-08). Observed: an echo already stored in the Matrix timeline whose message mapping (`Failed to save message part to database`) was not saved, because the core closed right after receiving it. Consequences:
   - The core drops log lines written after `Close` rather than failing on a closed file.
   - The tests accept only lines from aborted handlers, so anything else outliving `Close` fails them (checked by mutation).
   - An app killed by the system (mobile background) loses in-flight work the same way: consistency must come from resynchronising with the network after a restart, never from a graceful stop.
   - Upstream could drain portal events on stop. Reporting it needs the maintainer's permission.

## Measurements

Windows: Windows 11, Intel Core i7-9750H, MSYS2 UCRT64 GCC 16.2.0. Linux: Docker `golang:1.27.1` on the same machine (Debian, GCC 14.2.0). Go 1.27.1, mautrix-go v0.31.0, modernc.org/sqlite v1.60.1, mattn/go-sqlite3 v1.14.52; 2026-10-08. "Memory" is the process's private bytes on Windows (committed memory; the working set can be trimmed at any time) and its resident set on Linux. The two are not directly comparable.

| Measurement | Windows | Linux | Command |
|---|---|---|---|
| Library size, default build | 36.5 MB | 27.0 MB | `go build -buildmode=c-shared ./core/ffi` |
| Library size, stripped (`-trimpath -ldflags="-s -w"`) | 17.9 MB (7.1 MB gzip -9) | 19.4 MB (7.4 MB gzip -9) | same, with the flags |
| Dynamic dependencies of the library | `KERNEL32.dll`, UCRT | `libc.so.6` | `objdump -p`, `ldd` |
| Host memory once the library is loaded, before `musubee_open` | 41.8 MiB | 4.8 MiB | `TestSharedLibrary` |
| Memory added by opening a core (one network, empty database) | +15.4 MiB | +15.0 MiB | same |
| `musubee_open` / `musubee_close` | 78 to 85 ms / 6 to 41 ms | 21 to 94 ms / 6 to 10 ms | same, and the driver comparison runs |
| Ping through FFI (1 KiB payload, JSON both ways) | 14.3 to 16.7 µs per call | 8.1 to 8.9 µs per call | same |
| Leak check: 1,000,000 pings | +0.7 MiB memory, Go heap +0.0 MiB | — | `MUSUBEE_FFI_PINGS=1000000 MUSUBEE_FFI_ROUNDTRIPS=5000 go test -run TestSharedLibrary/RoundTrip -v ./core/ffi/` |
| Leak check: 100,000 pings (default test) | +0.2 to +0.8 MiB | +0.9 to +1.5 MiB | `TestSharedLibrary` |
| Same test on the CI runners (`windows-latest`, `ubuntu-latest`) | ping 13.3 µs, +0.2 MiB; round trip mean 7.5 ms (max 208 ms) | ping 8.9 µs, -0.9 MiB; round trip mean 2.3 ms | CI job "Go unit tests", step "Core as a C shared library" |
| Power of the leak check: the host skips `musubee_free` | +109.9 MiB over 100,000 pings | +103.1 MiB | `TestSharedLibrary/LeakIsDetected` |
| Round trip from C: send → echo connector → echo event read by `musubee_next_event` | mean 3.3 to 4.0 ms (5,000 runs: max 97 ms) | mean 2.9 to 3.4 ms | `TestSharedLibrary` |
| Memory over 5,000 round trips | +3.0 MiB; Go heap 1.6 MiB after, 7 goroutines | +1.2 to +1.9 MiB over 1,000 | same |
| SQLite driver, round trip from C (3 runs of 1,000) | modernc 3.3 ms; **mattn 16.2 ms**; mattn with `timeBeginPeriod(1)` 3.4 ms | modernc 2.9 to 3.0 ms; mattn 3.0 to 3.3 ms | temporary build tag, scratch script |
| SQLite driver, stripped library size | modernc 17.9 MB, mattn 15.6 MB | modernc 19.4 MB, mattn 16.9 MB | same |
| SQLite driver, memory once open | mattn 0.5 to 1.5 MB lower | mattn 1.3 to 1.9 MB lower | same |
| Race detector, Windows (now possible with the MSYS2 toolchain) | all core tests pass; `core/embedded` 30 runs without a race | in CI | `go test -race ./core/...` |

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| The core builds as a C shared library and is driven from a C program on Windows and Linux: requests, events, failures, close, calls on a closed handle | **verified** | `core/ffi/ffi_test.go` with `testdata/host.c`, local runs (Windows, Linux container) |
| No leak on the FFI path: memory flat over 1,000,000 calls, and the measurement detects a leak of the response buffers | **verified** on Windows (1,000,000) and Linux (100,000) | Measurements |
| The same build works on macOS (`libmusubee.dylib`, found through `@rpath`) | **verified** in CI on `macos-latest` (2026-10-08): 17.5 MiB default build, ping 5.7 µs, round trip 2.1 ms, memory flat over 100,000 pings, leak detected without `musubee_free` (+132.7 MiB). Not run on a local Mac | CI job "Go unit tests (macos-latest)", PR quentinemusee/musubee#14 |
| The library has no runtime dependency beyond the OS (`KERNEL32`, UCRT on Windows 10+, glibc on Linux) | **verified** for these builds; a Linux build against an older glibc than the users' is a packaging question (T1.5 / T1.8) | `objdump -p`, `ldd` |
| The mattn slowdown on Windows comes from the timer resolution in SQLite's busy handler | **verified** by experiment (`timeBeginPeriod(1)` removes it); the call path inside SQLite (`sqliteDefaultBusyCallback`, `winSleep`) is **assumed** from SQLite's design, not read for this ADR | Measurements |
| The 41.8 MiB of private bytes before `musubee_open` on Windows is the Go runtime's initial reservation | **assumed**: not broken down; Linux shows 4.8 MiB resident. To check if desktop memory matters (T1.5) | — |
| bridgev2 aborts in-flight portal events on stop, on v0.31.0 and `main` | **verified** by code reading (`Bridge.stop`, `Portal.eventLoop`, `getEventCtxWithLog`) and by the late log lines in tests | mautrix-go v0.31.0 and `main`, `bridgev2/bridge.go`, `portal.go` |
| Real networks resynchronise what an aborted handler lost | **unknown**: per connector (Telegram's update state, Signal's queue); must be tested by killing the core mid-message in each connector task | connector tasks |
| modernc.org/sqlite runs correctly on Android and iOS | **unknown**: it cross-builds (CI); runtime and memory checked in T1.3 and the iOS tasks. mattn stays the fallback | T1.3 |
| JSON over FFI is fast enough for the UI | **assumed**: 8 to 17 µs per 1 KiB call is far below a frame (16 ms); T1.4 compares with a local socket | Measurements |

## Consequences

- Electron (T1.5) and the mobile plugins can load the core as a native library with five C functions; an event-reading thread is part of each host's design.
- **Building the core with cgo on Windows needs the MSYS2 UCRT64 GCC first in `PATH` (or `CC` pointing to it).** An old 32-bit MinGW in `PATH`, as on the maintainer's machine, makes every cgo build fail with "64-bit mode not compiled in". Go enables cgo by default whenever it finds a `gcc`.
- `core/embedded` is a spike surface: T1.4 replaces it with the versioned contract and the domain layer (no Matrix IDs towards the UI, `CLAUDE.md` §2). Today its events still carry Matrix room and event IDs.
- The JSON event stream can drop events under backpressure: every UI must handle `overflow` by re-reading the state.
- Connector tasks must test a kill in the middle of a message (point 8).
- Revisit if: Go gains `dlclose` support; T1.4 picks a local socket instead of FFI (the library stays for embedding, its surface shrinks); modernc fails on a mobile target (switch to mattn behind `core/storage/sqlite`).

## Sources

- Go `cmd/cgo` documentation (`go doc cmd/cgo`: `//export` and its restriction on the preamble, `C.CBytes`), Go 1.27.1, read locally 2026-10-08.
- golang/go#11100, "runtime: support dlclose with -buildmode=c-shared", open (accessed 2026-10-08).
- gomuks, https://github.com/gomuks/gomuks, `pkg/ffi/ffi.go` (MPL-2.0; the repository is AGPL-3.0), read for its design only, 2026-10-08.
- mautrix-go v0.31.0 and `main` (2026-10-08), `bridgev2/bridge.go` (`stop`), `bridgev2/portal.go` (`eventLoop`, `getEventCtxWithLog`, `handleSingleEventWithDelayLogging`), MPL-2.0.
- modernc.org/sqlite v1.60.1 (BSD-3-Clause); mattn/go-sqlite3 v1.14.52 (MIT), https://github.com/mattn/go-sqlite3, DSN parameters from its README, 2026-10-08.
- MSYS2, https://www.msys2.org/ (installer `msys2-base-x86_64-20260927.sfx.exe`, SHA-256 checked; package `mingw-w64-ucrt-x86_64-gcc`), 2026-10-08.
- ADR 0001 (stack), ADR 0009 (bridgev2 in process).
