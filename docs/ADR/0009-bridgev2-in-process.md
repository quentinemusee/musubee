# ADR 0009 — Running bridgev2 connectors in-process, without a homeserver

- **Status**: accepted
- **Date**: 2026-10-08
- **Task**: T1.1
- **Deciders**: Quentin Raimbaud (maintainer), with the design assistant

## Context

The "on-device" connection mode (`CLAUDE.md` §2) runs the bridge inside the app: the bridge talks to the network (Telegram, Signal…) directly, and no server sees the messages. mautrix-go's `bridgev2` framework is designed for a different deployment: a long-running process registered as an **application service** on a Matrix homeserver, which relays every event through the homeserver.

Open question 1 of `CLAUDE.md` §4: can a `bridgev2` connector run **without a homeserver**, in the same process as the client? T1.1 answers it with a proof of concept: our own echo connector (ADR 0004 replaced Beeper's unlicensed dummybridge with it), run in-process, a message going through the connector and reaching local storage, without Synapse.

## How bridgev2 reaches Matrix

Read in mautrix-go v0.31.0, the latest release (2026-09-16):

- `bridgev2` talks to Matrix only through two interfaces, `MatrixConnector` (the bridge-wide side: ghost IDs, bot, statuses, room state) and `MatrixAPI` (the actions of one user: send, create a room, upload media…), in `bridgev2/matrixinterface.go`. `bridgev2.NewBridge` takes any `MatrixConnector`. The appservice implementation (`bridgev2/matrix`) is one implementation among others; nothing else in `bridgev2` imports it.
- `Bridge.QueueMatrixEvent` (`bridgev2/queue.go`) is the entry point for events coming from Matrix. The appservice connector calls it for each event the homeserver pushes; our implementation calls it when the user sends a message.
- Room creation (`Portal.createMatrixRoomInLoop`, `bridgev2/portal.go`) asks the connector for a deterministic room ID (`GenerateDeterministicRoomID`) and, when the connector advertises `AutoJoinInvites`, lists the initial members instead of inviting them one by one.
- Double puppeting (`User.DoublePuppet`, `bridgev2/user.go`) asks `MatrixConnector.NewUserIntent` for an API acting as the user.

## Options considered

### A. A real homeserver on the device (for example a minimal Synapse or Conduit) + the appservice connector
- Pros: the unmodified upstream path.
- Cons: a second server process on a phone, with its own database, HTTP stack and memory; the iOS notification extension (`CLAUDE.md` §3) could never fit it. Rejected.

### B. A minimal local implementation of `MatrixConnector` / `MatrixAPI` (chosen)
- Pros: no extra process, no network on the Matrix side, one SQLite database; the storage is shaped for the app (timeline, statuses, profiles) rather than for a homeserver.
- Cons: we maintain an implementation of an upstream interface, which can change between mautrix-go releases. Mitigation: the interfaces are small, our tests run the real `bridgev2` code end to end, and `var _ bridgev2.MatrixConnector = …` assertions break the build when the interface changes.

### C. Embedding `bridgev2`'s appservice connector against a fake in-process HTTP homeserver
- Pros: zero changes to the upstream connector.
- Cons: serialises every event to JSON and back through a loopback HTTP server to reach our own storage; we would implement a subset of the client-server API instead of two Go interfaces. More code, slower, harder to test. Rejected.

## Decision

**Feasible: go.** `bridgev2` connectors run in-process on top of `core/localmatrix`, our local implementation of the Matrix side, with SQLite storage and no homeserver. The proof of concept passes (see Measurements).

Design, as implemented:

1. **`core/localmatrix`**: one `Server` holds the storage shared by every network; each `bridgev2.Bridge` gets its own `Connector` (`MatrixConnector`) and `Intent`s (`MatrixAPI`). Rooms, events (with a monotonic stream order), room state, message statuses, bridge states, profiles, room account data (read receipts, tags, mute) and media live in `local_*` tables. The app reads them through `Server` methods and subscribes to changes (`Server.Subscribe`); the storage stays the source of truth, and a subscriber that falls behind is dropped and must re-read it.
2. **The user** is `@me:musubee.local`. `musubee.local` never resolves to a real server. Each network is one bridge with a bridge ID of lowercase letters and digits (`echo`, later `telegram`, `signal`…); its bot is `@<id>bot:musubee.local` and its ghosts `@<id>_<escaped remote ID>:musubee.local`. Bridge IDs that would let a bridge impersonate the user are refused.
3. **Sending**: `Server.SendMessage` stores the event with a `PENDING` status, then hands a copy to `Bridge.QueueMatrixEvent`. The outcome arrives through `SendMessageStatus` and is stored (`SUCCESS`, or a failure with a reason and a human-readable message). If `bridgev2` drops the event without reporting a status (portal being deleted, bridge shutting down), the message is marked failed rather than left pending forever.
4. **Receiving**: `bridgev2` sends the converted remote messages through the ghost's `Intent`, which stores them; invited users join immediately (`AutoJoinInvites`), like the appservice API joins ghosts on demand.
5. **Double puppeting**: `NewUserIntent` returns an intent for the user itself, so messages the user sends from another device of the same network appear as theirs.
6. **Commands are disabled**: the app drives logins and settings through the core API, never through chat commands. `bridgev2` treats every message starting with the command prefix as a command, and an empty prefix would match every message, so the prefix is a string no message starts with in practice, and the user has no `Commands` permission.
7. **Asynchronous portals**: `PortalEventBuffer` is 64 (the upstream default): sending returns once the event is queued, and the result arrives as a status.
8. **One SQLite database** for the local Matrix tables and every bridge (rows carry the bridge ID). The bridges are marked `ExternallyManagedDB`: the host upgrades the tables and closes the database, so that stopping one network does not close the database under the others. Each bridge is stopped and started with the host; a stopped host is not reused.
9. **Pure-Go SQLite** (`modernc.org/sqlite`, BSD-3-Clause, SQLite transpiled to Go), so that the core builds without cgo for every target; WAL journal, `synchronous=NORMAL`, foreign keys on, 10 s busy timeout, immediate write transactions. `core/storage/sqlite` is the only place that knows the driver: replacing it (for example by an encrypted build, see Unknown) does not touch the rest.
10. **Not supported yet**, each refused explicitly rather than faked: direct media (`ErrDirectMediaNotEnabled`), batch sending for backfill (`ErrBatchSendUnsupported`), encrypted media from the network side; typing notifications are accepted and dropped (the event stream to the UI is T1.4).
11. **Upstream behaviours we compensate for**, each covered by a test:
    - `Bridge.Stop` disconnects logins but never destroys their bridge state queues (it only does so on logout): in a bridge process, stopping means exiting. In the app, the core stops and starts within one process (closing the window, mobile background), and each queue would keep a goroutine holding the whole bridge. `Host.Stop` destroys them, with the same steps as `UserLogin.Delete`. Upstream fixed it after v0.31.0 (commits "stop bridge state queues on bridge stop" and mautrix/go#572, 2026-09-18 and 19, not released yet); its `Destroy` becomes idempotent there, so our call stays harmless and can be removed after the upgrade.
    - `Bridge.Stop` closes the database unless `ExternallyManagedDB` is set (point 8).
    - **Data race on shared ghosts.** `Ghost.UpdateInfoIfNecessary` reads a ghost's profile fields and `Ghost.UpdateInfo` writes them, without a common lock. bridgev2 creates portals in parallel (one goroutine per portal event), and every DM lists the user's own ghost: the first update of that ghost races. The race detector found it in CI (20 reports in 3 runs). mautrix/go#571 (2026-09-17, not released) makes `UpdateInfo` hold the ghost's lock, but the unlocked read at the start of `UpdateInfoIfNecessary` remains on `main`. The echo connector updates the user's own ghost once, before announcing its conversations, so that the portals only read it afterwards. Real connectors share ghosts too (the user's own ghost, a contact present in several groups): each must pass the race detector before it ships.
12. **Go 1.27.1 or later is required.** Go 1.27.0's `database/sql` can deadlock forever when rows are read and closed concurrently: a lost wakeup in the new `closingMutex` (golang/go#81043, backported to 1.27.1 in golang/go#81151). Our round-trip benchmark hit it about once every few thousand messages: the portal's event loop blocked forever in `Rows.Err`, and the echo never arrived. Every `go.mod` and `go.work` now declares `go 1.27.1` (CI reads the version from `infra/go.mod`), and a test fails on a 1.27.0 toolchain.

### The echo connector (`core/connector/echo`)

Our own `bridgev2` network connector (AGPL-3.0-or-later, written without reading dummybridge, whose code has no license). One login flow (any username), three contacts with a direct conversation each: **Instant Echo** answers every message immediately, **Delayed Echo** after a configurable delay (2 s by default), **Unreachable Contact** rejects every message with a permanent network error and a message for the user. Disconnecting drops pending delayed echoes, as a real network would lose them. It is the test network of every phase 1 task that needs one.

## Measurements

Windows 11, Intel Core i7-9750H, Go 1.27.1, mautrix-go v0.31.0, modernc.org/sqlite v1.60.1, 2026-10-08.

| Measurement | Result | Command |
|---|---|---|
| End-to-end tests (`core/bridgehost`, 9 scenarios) and unit tests (`core/localmatrix`, `core/storage/sqlite`) | all pass; 10 consecutive runs without failure (34 s) | `go test -count=10 ./core/...` |
| Round trip: user message → echo connector → echo stored and delivered to a subscriber | 3.6 to 4.0 ms per round trip (5 runs of 2,000, Info log level) | `go test -run '^$' -bench RoundTrip -benchtime 2000x -count 5 ./core/bridgehost/` |
| Live heap after 2,000 round trips (one network, one login, three conversations) | 1.5 to 1.6 MiB, stable across 5 runs in one process (each run starts and stops a host); it includes the test harness's in-memory log buffer. Before the bridge state queue fix (Decision, point 11) it grew by about 0.1 MiB per host | same benchmark, `heap-MiB` |
| Stress: round trips without a lost echo, after requiring Go 1.27.1 | 60,000 round trips (6 × 5 × 2,000, Debug log level) without a lost echo; on Go 1.27.0, about one lost echo every 2,000 to 5,000 | same benchmark, repeated |
| Race detector on the core (Linux container, `golang:1.27.1`) | 20 runs without a race or a failure; without the ghost workaround (point 11), 20 race reports in 3 runs | `docker run --rm -v <repo>:/src -w /src golang:1.27.1 go test -race -count=20 ./core/...` (also in CI on three OSes) |
| Pure-Go builds of the core (`CGO_ENABLED=0`) | windows/amd64, linux/amd64, darwin/amd64 and arm64, android/arm64, ios/arm64: build | `GOOS=… GOARCH=… CGO_ENABLED=0 go build ./core/...` (also in CI) |

These figures are for the in-process path only (no real network). Memory on a phone, binary size and the shared library are measured in T1.2, T1.3 and T1.7.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| `bridgev2` reaches Matrix only through `MatrixConnector` / `MatrixAPI`, and runs with our local implementation: login, portal creation, sending, receiving, failures, restart, two networks in one process | **verified** (code read, tests pass) | mautrix-go v0.31.0 `bridgev2/matrixinterface.go`, `bridge.go`, `queue.go`, `portal.go`; `core/bridgehost/bridgehost_test.go` |
| `bridgev2`, the echo connector and our local Matrix layer compile without cgo for every target OS | **verified** (cross-builds) | local runs and CI job "Go unit tests" |
| After a restart from the same database, the login reconnects, no room is duplicated and the history is kept | **verified** | `TestRestartKeepsHistoryAndLogin` |
| No message content reaches the logs on the bridged path, at trace level (rule 8), with a positive control proving the logs cover that path | **verified** for the echo connector | `TestMessageContentIsNotLogged`; real connectors must be checked again (T1.4 and the connector tasks) |
| Go 1.27.0's `database/sql` deadlock (golang/go#81043) is what lost echoes; 1.27.1 fixes it | **verified** (goroutine dump at the failure matched the upstream description; no failure in the stress run with 1.27.1) | goroutine dump, 2026-10-08; matrixorigin/matrixone#29692 |
| The data race on shared ghosts comes from bridgev2, not from our code, and is still on mautrix-go `main` (the read side) | **verified** by code reading and by the race detector, on v0.31.0 and on `main` (commit cc1893852bcf, 2026-10-08: 10 race reports in 5 runs without our workaround) | `bridgev2/ghost.go`, `UpdateInfoIfNecessary` and `UpdateInfo`; mautrix/go#571 |
| Real connectors (mautrix-telegram, -signal…) run the same way: they only use `bridgev2` and their own tables | **assumed**: they are `bridgev2` connectors like ours, but each may rely on features we refuse (direct media, batch send for backfill). Checked per connector in its task | — |
| `modernc.org/sqlite` is fast enough and small enough on mobile | **verified on the desktop** (ADR 0010: as fast as `mattn/go-sqlite3` on Linux, faster on Windows, 2.3 to 2.6 MB larger); **unknown** on mobile (T1.3) | ADR 0010, T1.3 |
| Encryption at rest of the SQLite file (SQLCipher or OS-level protection) | **unknown**: modernc has no SQLCipher; options to compare when the storage design is settled (iOS Data Protection and Android file-based encryption cover the device-locked case). Needs its own ADR before real accounts are stored | phase 2 |
| Network-specific tables of real connectors coexist in the shared database | **assumed**: they use their own version tables (`db.Child`); one bridge per network, never two bridges of the same network | connector tasks |
| Backfill (history) works | **unknown**: needs batch sending or the non-batch backfill path; not tested with the echo connector | T1.4 or the first real connector |

## Consequences

- The core embeds bridges as a library: the "on-device" mode needs no server at all, and the same `bridgehost` can later run on a server for the "hosted bridge" mode.
- `localmatrix` is Matrix-shaped internally (room and event IDs) but stays inside the core: the domain layer (T1.4) maps it to `Conversation`, `Message`, `Person` and `Account`, and no Matrix type reaches the UI (`CLAUDE.md` §2).
- We follow mautrix-go releases deliberately: a new version can change the interfaces (the build breaks) or their behaviour (the end-to-end tests should catch it). Read the changelog before upgrading.
- Upstream: the bridge state queue leak is already fixed on mautrix-go `main` (not reported by us). Reported with the maintainer's permission on 2026-10-08: the remaining read race in `Ghost.UpdateInfoIfNecessary` (mautrix/go#598), and `dbutil.WithFSPath` joining `embed.FS` paths with `filepath.Join`, which breaks on Windows (mautrix/go-util#46; we use `fs.Sub` instead).
- `bridgev2` creates a portal's room through our `CreateRoom` before it records the room as the portal's (`createMatrixRoomInLoop`): for a moment the room exists but a message sent to it fails with `ErrNoPortal`. Found in CI during T1.2 (`TestDelayedEcho`, once); `Server.SendMessage` now waits, within a bound, until `bridgev2` knows the portal.
- When upgrading past v0.31.0: remove the bridge state queue workaround in `Host.Stop`, keep the ghost workaround until the read race is fixed upstream.
- Revisit if: `bridgev2` gains an official non-homeserver mode, or the interfaces grow enough that maintaining `localmatrix` costs more than option C.

## Sources

- mautrix-go v0.31.0, https://github.com/mautrix/go/tree/v0.31.0/bridgev2 (MPL-2.0), read from the Go module cache, 2026-10-08: `matrixinterface.go` (interfaces, `MatrixCapabilities.AutoJoinInvites`), `bridge.go` (`NewBridge`, `StartConnectors`, `stop`, `ExternallyManagedDB`), `queue.go` (`QueueMatrixEvent`, command prefix, `QueueRemoteEvent`), `portal.go` (`queueEvent`, `createMatrixRoomInLoop`), `bridgestate.go`, `userlogin.go` (`Delete`, `Disconnect`), `user.go` (`DoublePuppet`).
- go.mau.fi/util v0.10.1, `dbutil/upgradetable.go` (`WithFSPath`), 2026-10-08.
- modernc.org/sqlite v1.60.1, https://gitlab.com/cznic/sqlite, `LICENSE`, `LICENSE-3RD-PARTY.md`, 2026-10-08.
- Go 1.27.0 `database/sql/closemu.go`; golang/go#81043 and its 1.27.1 backport golang/go#81151, as cited by https://github.com/matrixorigin/matrixone/pull/29692 (accessed 2026-10-08); Go downloads, https://go.dev/dl/ (1.27.1 is the current stable release, accessed 2026-10-08).
- ADR 0004 (echo connector instead of dummybridge), ADR 0001 (stack).
