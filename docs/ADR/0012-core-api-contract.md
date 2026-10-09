# ADR 0012 — The core API contract: one JSON Schema, generated types, and the transports that carry it

- **Status**: accepted
- **Date**: 2026-10-09
- **Task**: T1.4
- **Deciders**: Quentin Raimbaud (maintainer), with the design assistant

## Context

Every user interface talks to the same Go core: the Electron app on the desktop (T1.5), the Capacitor app on Android and iOS, and later a web app talking to a hosted core (`CLAUDE.md` §1 and §2). T1.2 and T1.3 used a throwaway JSON surface to measure the shared library (ADR 0010) and the JNI binding (ADR 0011). T1.4 replaces it with the real contract, and settles two questions:

1. **How the contract is written down**, so that the Go side and the TypeScript side cannot drift apart. The deliverable asks for a versioned schema of commands and event streams, a TypeScript type generator, and contract tests on both sides against the same schema.
2. **How the bytes travel**: direct FFI into the core loaded in the UI's process, or a local socket to a core in another process. `CLAUDE.md` §4 question 4.

Constraints:

- **No Matrix type crosses the contract** (`CLAUDE.md` §2): the UI sees accounts, conversations, messages and login steps, never rooms, events or MXIDs. This is what lets an account switch between on-device and hosted bridges.
- **The same contract on every transport**: in-process on mobile (iOS has no child processes, and Android keeps the core in a foreground service of the app's own process, ADR 0011), possibly a child process on the desktop, a network connection to a hosted core later.
- **Licenses**: every dependency AGPL-3.0-compatible (`CLAUDE.md` §6.4).
- **No message content in logs** (`CLAUDE.md` §6.8).

## Options considered

### How the contract is written down

#### A. A JSON Schema as the source of truth, with generated Go and TypeScript types (chosen)

`core/api/schema/core-api.schema.json` (JSON Schema draft 2020-12) holds:

- the three envelopes: `Request` (`id`, `command`, `params`), `Response` (`id` and exactly one of `result` or `error`) and `Event` (`type`, `data`);
- the table of commands (`$defs/Commands`: params and result of each) and the table of events (`$defs/Events`: data of each);
- the domain types: `Network`, `LoginFlow`, `LoginStep`, `Account`, `Conversation`, `Message`, the error codes.

An in-house generator (`core/api/internal/codegen`, run by `go generate ./core/api`) writes `core/api/types.gen.go` and `ui/src/core-api/types.gen.ts`. It understands a deliberately small subset of JSON Schema: named object and string-enum types, properties of primitive, array, map or `$ref` type, and the two tables. Keywords that only validate (`pattern`, `minimum`, `if`/`then`…) are ignored; any other keyword is an error, so the schema cannot say more than the types without the generator noticing.

Pros:

- One file to read and to review for every change of the contract, written in a standard any tool can validate against.
- The generated TypeScript has a mapped type per table (`Commands[K]`, `Events[K]`), so `client.call("messages.send", params)` is typed end to end, and a command added to the schema breaks the typecheck of the code that must handle it.
- A JSON Schema validator on each side can check real traffic, independently of the generated types.

Cons:

- About 700 lines of generator (with its tests) to maintain.
- JSON Schema can say things the types cannot (conditional requirements of `LoginStep`): those are checked by the validators only, not by the compiler.

#### B. Generated from Go types (reflection or an existing schema generator)

The Go structs would be the source, and a schema or TypeScript types would be derived from them.

- Pro: no separate file; Go is where the logic lives.
- Con: the contract would be whatever the Go code happens to marshal. Go's `omitempty`, `nil` slices as `null` and embedded structs leak into the wire format, and a reviewer has to read Go to know the contract. The UI side would depend on the Go side's conventions.

#### C. An off-the-shelf generator per language

For example `json-schema-to-typescript` for the UI and a separate JSON-Schema-to-Go tool for the core.

- Pro: no generator of our own.
- Cons: two tools, two interpretations of the same schema, two naming schemes; neither produces the command and event tables as mapped types; two more build dependencies to audit. **Not trialled**: the in-house subset was small enough that its cost is known (Consequences).

#### D. Protocol Buffers or another IDL

- Pro: compact binary encoding, mature generators.
- Cons: a binary format is hard to read in logs and tests, the measurements below show encoding is not the bottleneck, and the web app would need a protobuf runtime. A later binary encoding of the same messages stays possible.

### How the bytes travel

All options carry the same JSON documents. Measured from Node 24 (the runtime of Electron's main process) with `scripts/transportbench` (Measurements):

| Transport | How | Notes |
|---|---|---|
| **ffi-sync** | the shared library of ADR 0010 loaded with koffi (MIT), called on the JavaScript thread | blocks the event loop for the length of each call: unusable for calls that wait (`login.wait`, slow sends) |
| **ffi-async** | the same library, each call on a koffi worker thread | the only safe in-process variant |
| **stdio** | the core in a child process, newline-delimited JSON on its standard input and output | `core/api/transportbench -listen stdio` |
| **tcp** | the same child process on a loopback TCP port, behind a random token | any local process can reach a loopback port: a token is mandatory |

On mobile, only in-process applies: iOS apps cannot start child processes, and on Android the core lives in the app's foreground service (ADR 0011). The JNI binding of ADR 0011 now carries the T1.4 contract.

## Decision

1. **The contract is `core/api/schema/core-api.schema.json`** (option A). The Go types (`core/api`) and the TypeScript types (`ui/src/core-api`) are generated from it and checked in; a test fails when they are stale.
2. **Shape of the protocol:**
   - Requests carry a caller-chosen integer `id`, copied into the response. Requests may run concurrently; responses can come back in any order.
   - Errors carry a stable `code` (`invalid_request`, `unknown_command`, `invalid_params`, `not_found`, `unsupported`, `timeout`, `cancelled`, `closed`, `network`, `internal`) and a `message` for developers. UIs show their own text per code.
   - Events come on a separate stream. The core queues up to 1024 of them; when the UI falls behind, the oldest are dropped and `resync.required` is queued instead, after which the UI re-reads the state with the `*.list` commands. `core.closed` is always the last event.
   - IDs are opaque strings with a type prefix (`a.` account, `c.` conversation, `m.` message, `k.` cursor, `p.` login process). They encode Matrix identifiers today, but the UI must not parse them. An ID that names nothing, malformed or not, is `not_found`. A malformed cursor is `invalid_params`: a cursor names no object.
   - Long waits are explicit requests: `login.wait` blocks until the login step changes, and `login.cancel` ends it with `cancelled`.
3. **Versioning**: `core.hello` returns `api_version`, `"major.minor"` (`1.0` here; `1.1` since T1.6, which added `accounts.logout`, ADR 0014). A UI calls it first and refuses a core with another major version.
   - A minor version only adds: optional fields, commands, event types, enum values. Consumers ignore what they do not know: `parseEvent` returns `null` for an unknown event type.
   - Anything else (removing or renaming a field, making a field required, changing a meaning) is a new major version.
   - Inside one version, every object is exact (`additionalProperties: false`), so tests catch a field that the schema does not declare.
4. **Contract tests on both sides, with two independent validators:**
   - Go: `santhosh-tekuri/jsonschema` v6 (Apache-2.0) checks the shared examples (`core/api/schema/examples.json`, valid and invalid), and **every response and event of the real core** in the `core/embedded` tests. The C host test (`core/ffi`) and the Android instrumented tests drive the same commands through their transports.
   - TypeScript: Ajv 8 (MIT, strict mode) checks the same examples, then one example per command and per event written against the generated types (the mapped types force one for each), and the envelopes. `vitest` runs them; `tsc` type-checks them.
5. **Transports:**
   - **Mobile: in-process**, through the shared library: JNI on Android (ADR 0011), the C interface on iOS (T1.7).
   - **Desktop: the core in a child process, newline-delimited JSON over stdio** (the `stdio` row). T1.5 builds it into the Electron shell. Reasons:
     - **Speed is not the criterion.** Every transport adds less than a tenth of a millisecond to a call, and the echo round trip costs about 4 ms whatever carries it (Measurements). The safe in-process variant, ffi-async, costs as much as stdio (93 µs against 124 µs per 1 KiB call), because it also crosses threads.
     - **Crash isolation.** A Go fatal error (a runtime throw, an out-of-memory, a crash in C code under cgo) cannot be recovered and kills the process that loaded the library: Electron's main process, and with it every window. A child process can die and be restarted, and the UI resynchronises with the `*.list` commands.
     - **No Go runtime inside Electron's main process.** The Go runtime starts its own threads, and the library can never be unloaded (ADR 0010). Keeping it in its own process leaves Electron's process to Node and Chromium.
     - **The path to a remote core.** A stream of newline-delimited JSON documents is what a hosted core will speak over a WebSocket or TLS connection; the desktop then has one code path for "the core is local" and "the core is remote".
     - **stdio rather than a local socket.** The pipes are private to the parent and the child. A loopback port is reachable by every local process and needs a token, and was the slowest in the measurements. Named pipes and Unix sockets were not measured: they would need their own access control too, and offer nothing that stdio lacks for one client.
   - The shared library stays the only binding package (`core/ffi`), and the desktop app may still load it in-process if T1.5 finds a blocker in the child process (signing a helper executable, start-up time).
6. **The `debug.*` commands** (`debug.ping`, `debug.stats`) are part of the contract, for measurements and health checks. They return no user data.
7. **SQLite connection pool**: the core keeps up to 8 idle connections for 5 minutes (`core/storage/sqlite`), instead of database/sql's default of 2 (Measurements).

## Measurements

**Hardware and versions:** Windows 11, Intel Core i7-9750H, Node v24.20.0, Go 1.27.1, koffi 3.3.2, MSYS2 UCRT64 GCC 16.2.0 for the shared library. Date: 2026-10-09.

**Command:** `cd scripts/transportbench && npm ci && node bench.mjs`, three times in a row. Each transport gets a fresh core and data directory. Per transport:

- `core.hello`, then 1,000 warm-up and **20,000 timed `debug.ping` calls with a 1 KiB payload**, one at a time, timed from the JavaScript side (JSON encoding, transport and decoding both ways);
- a login to the echo network, then 10 warm-up and **200 timed round trips**: `messages.send` to the "Instant Echo" contact, until its `message.added` event arrives.

| Transport | Ping, mean of 3 runs (min to max) | Round trip, mean of 3 runs (min to max) | Round trip, median per run | Round trip, slowest per run |
|---|---|---|---|---|
| ffi-sync | **31 µs** (28 to 35) | 3.86 ms (3.73 to 4.08) | 3.53 to 3.89 ms | 11.8 to 14.3 ms |
| ffi-async | 93 µs (85 to 100) | 4.27 ms (3.60 to 5.23) | 3.41 to 4.43 ms | 8.8 to 21.8 ms |
| stdio | 124 µs (114 to 133) | 4.27 ms (4.10 to 4.45) | 3.83 to 4.19 ms | 10.6 to 13.3 ms |
| tcp | 161 µs (141 to 174) | 4.21 ms (3.96 to 4.60) | 3.59 to 4.33 ms | 11.1 to 11.8 ms |

Reading:

- **The transports differ by about 0.1 ms per call; the round trip is the core's work.** The spread between runs of one transport (up to 1.6 ms for ffi-async) is larger than the spread between transports.
- A UI frame is 16 ms. A screen that issues a handful of calls spends well under a millisecond in the transport, whichever it is.
- On Android, the same 1 KiB ping through JNI took 44 µs and the round trip 3.9 ms on the emulator (ADR 0011).

**SQLite connection pool.** With the T1.4 core, the C host test's echo round trip rose from 3.3 ms (ADR 0010) to about 11 ms on Windows. The core now runs a few more queries per message, and database/sql keeps only 2 idle connections by default: the others were closed after each query and reopened for the next. Opening a SQLite connection reopens the database file and its write-ahead log, which is slow on Windows (`CreateFileW`). With 8 idle connections, the C round trip is 4.1 ms and the `core/embedded` benchmark about 3.5 ms. `TestPoolKeepsIdleConnections` fails without the setting. Idle connections close after 5 minutes, so that an idle app gives their page caches back.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| Every response and event of the core matches the schema, for every command | **verified** by the `core/embedded` tests, which validate all traffic with the Go validator | `go test ./core/embedded/` |
| The Go and TypeScript validators agree on the shared examples | **verified** (Go: `core/api` tests; TypeScript: `ui/src/core-api/contract.test.ts`) | `go test ./core/api/`, `npm test` in `ui/` |
| The generated types cannot build a document the schema rejects, for the examples written against them | **verified** for one example per command and per event; the types do not express the conditional rules of `LoginStep`, which only the validators check | `contract.test.ts` |
| The C interface and the Android JNI binding carry the new contract | **verified** (`core/ffi` C host test; 8 instrumented tests on the API 30 x86 emulator) | 2026-10-09 |
| Transport costs from Node on Windows | **verified** (Measurements) | `scripts/transportbench` |
| The same ordering on Linux and macOS, and inside Electron rather than plain Node | **assumed**: the transports are the same system primitives, and Electron's main process is Node; to check in T1.5 | T1.5 |
| koffi works in Electron's main process | **assumed** (it is a Node-API addon); only matters if T1.5 falls back to in-process | T1.5 |
| A child process can be signed and notarised inside the macOS app, and starts fast enough | **unknown** | T1.5 |
| The protocol is enough for real networks (Telegram login with a code and a password, media, typing) | **unknown**: the echo network exercises the username and code flows only. New commands and fields are minor versions. *Update T1.6 (ADR 0014): Telegram's bot login and messages go through unchanged; only `accounts.logout` was missing (version 1.1). Phone login with a code and a password, media and typing are still not exercised.* | connector tasks |
| A hosted core can speak the same documents over the network | **assumed**: the envelopes are transport-independent; authentication and reconnection are not designed | later phase |

## Consequences

- The UI never sees a Matrix type: the schema is where that rule is enforced and reviewed.
- Changing the contract means editing the schema, running `go generate ./core/api`, and adding examples to `examples.json`; the tests of both sides then say what else must change. `ui/` gets its first `package.json` (TypeScript, Vitest, Ajv), and CI runs its tests and typecheck.
- The generator is ours: it stays small only if the schema keeps to its subset. Revisit (a standard generator, or a richer subset) if the schema needs `oneOf` between object types or generic types.
- The desktop app will supervise a child process: start, restart after a crash, and pass its data directory. T1.5 measures its start-up time.
- `scripts/transportbench` and `core/api/transportbench` stay as measurement tools. The real sidecar comes with T1.5.
- **Revisit if:** the child process causes a blocker in T1.5 (signing, start-up, antivirus); a feature needs to share memory with the UI (large media buffers: pass file paths, not bytes, first); a binary encoding becomes worth its cost (profiling shows JSON on a hot path).

## Sources

Accessed 2026-10-09.

- JSON Schema draft 2020-12: https://json-schema.org/draft/2020-12
- `github.com/santhosh-tekuri/jsonschema/v6` v6.0.3, Apache-2.0: https://github.com/santhosh-tekuri/jsonschema
- Ajv 8.20 (MIT), strict mode and `strictRequired`: https://ajv.js.org/strict-mode.html
- koffi 3.3.2 (MIT), asynchronous calls: https://koffi.dev/
- Go `database/sql`, `DB.SetMaxIdleConns` (default 2) and `SetConnMaxIdleTime`: https://pkg.go.dev/database/sql
- Go `os/signal`, "Go programs that use cgo or SWIG" and "Non-Go programs that call Go code": https://pkg.go.dev/os/signal
- ADR 0010 (C shared library) and ADR 0011 (Android)
