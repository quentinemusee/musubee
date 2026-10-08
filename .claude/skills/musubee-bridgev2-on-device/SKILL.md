---
name: musubee-bridgev2-on-device
description: How Musubee runs mautrix-go bridgev2 network connectors in-process, without a Matrix homeserver (packages core/localmatrix, core/bridgehost, core/connector/echo). Use this skill whenever you touch those packages, add or embed a network connector (Telegram, Signal, WhatsApp…), upgrade mautrix-go, debug a message that never gets its echo or status, change how the core starts and stops, or write tests that send messages through a bridge, even if the request does not say "bridgev2".
---

# Running bridgev2 connectors on the device

Musubee's "on-device" mode runs each network's `bridgev2` connector inside the core, with no homeserver and no appservice. The decision, the measurements and the sources are in `docs/ADR/0009-bridgev2-in-process.md`; read it before changing the design. This skill is the working knowledge needed to change the code without breaking it.

## The shape

- `bridgev2` reaches Matrix only through two interfaces, `MatrixConnector` and `MatrixAPI` (`bridgev2/matrixinterface.go` in mautrix-go). `core/localmatrix` implements them on SQLite: one `Server` for the whole core, one `Connector` per bridge (`Server.NewConnector`), `Intent`s for the bot, the ghosts and the user.
- `core/bridgehost.Host` owns the bridges: one `bridgev2.Bridge` per **network** (bridge ID `echo`, `telegram`…), all on one shared `*dbutil.Database`. One bridge serves every login of its network; never create two bridges for the same network.
- The app (later: the domain layer of T1.4) reads `Server.Rooms`, `Timeline`, `MessageStatus`… and subscribes with `Server.Subscribe`. The storage is the source of truth: a subscriber that falls behind is dropped and must re-read it.
- Sending: `Server.SendMessage` stores the event as `PENDING`, then calls `Bridge.QueueMatrixEvent`. The final status arrives later through `MatrixConnector.SendMessageStatus`. Never assume a send succeeded because `SendMessage` returned.
- `core/connector/echo` is the test network: contacts `ContactInstant`, `ContactDelayed`, `ContactUnreachable`. Use it for any test that needs a network.

## Invariants (each one broke something before)

1. **Go 1.27.1 or later.** 1.27.0's `database/sql` loses a wakeup in its close mutex (golang/go#81043): a portal's event loop blocks forever in `Rows.Err`, about once every few thousand messages, and the echo never comes. `core/storage/sqlite` has a test that fails on 1.27.0. Do not lower the `go` line of any `go.mod`.
2. **`ExternallyManagedDB = true`** on every bridge, and the host runs `br.DB.Upgrade` itself before `br.Start`. Otherwise `Bridge.Stop` closes the shared database under the other networks ("sql: database is closed").
3. **`Host.Stop` destroys each login's `BridgeStateQueue`** after `Bridge.Stop`. Upstream only does it on logout, so every stop/start cycle in one process would leak a goroutine holding the whole bridge. `leak_test.go` catches a regression.
4. **Events `bridgev2` drops without a status** (portal being deleted, bridge shutting down: `QueueMatrixEvent` returns neither queued nor success) are marked failed by `SendMessage` (`failIfPending`). Keep that path: a message must never stay pending forever.
5. **Bridge states from a previous run are deleted** by `Server.Upgrade`: they describe a process that no longer exists.
6. **Commands are off**: the command prefix is a string no message starts with, and the user has no `Commands` permission. An empty prefix would turn every message into a command.
7. **Room IDs are deterministic** (`GenerateDeterministicRoomID`) and `AutoJoinInvites` is advertised: restarting does not duplicate rooms, and ghosts are members as soon as the room exists.
8. **Unsupported features fail loudly**: direct media, batch send (backfill), encrypted media from the network. When a real connector needs one, implement it with a test; never return fake success.
9. **No message content in logs** (`CLAUDE.md` §6.8). `TestMessageContentIsNotLogged` runs the bridged path at trace level with a positive control; extend it when you add a connector.

## Adding a real network connector

1. Check its license (AGPL-compatible; mautrix bridges are AGPL-3.0) and add it to `NOTICE`; run `go run ./scripts/licenseaudit`.
2. `Host.AddNetwork("<id>", connector)` before `Host.Start`. Bridge IDs are lowercase letters and digits; IDs that would clash with the user or another network's ghosts are refused.
3. Check what the connector needs from the Matrix side (`grep` for `ErrDirectMediaNotEnabled`, `BatchSend`, `GetCapabilities` in its code) against invariant 8.
4. Its own tables go into the shared database through `db.Child` with their own version table: verify they upgrade cleanly next to the others.
5. Build the core with `CGO_ENABLED=0` for every target (the CI step "Pure-Go builds of the core"): a connector that needs cgo breaks the pure-Go promise and needs an ADR.

## Upgrading mautrix-go

Read the changelog, then: `go build ./core/...` (the `var _ bridgev2.MatrixConnector = …` assertions break on interface changes), `go test -count=3 ./core/...`, the benchmark, and re-check the invariants above against the new `bridge.go` (`StartConnectors`, `stop`) and `bridgestate.go`: if upstream now destroys bridge state queues on stop, remove our workaround.

## Testing

- Real SQLite in `t.TempDir()`, the real `bridgev2`, the echo connector: no mocks (`CLAUDE.md` §6.2). Helpers in `core/bridgehost/bridgehost_test.go` (`startHost`, `login`, `room`, `send`, `waitForStatus`, `waitForMessageFrom`).
- Wait on conditions (`waitFor`), never on sleeps: everything after `QueueMatrixEvent` is asynchronous.
- A flaky "no echo" is a bug, not noise: dump the goroutines (`runtime.Stack(buf, true)`) at the timeout and look for a goroutine stuck in `database/sql` or in `portal.queueEvent`.
- Commands: `go test -count=1 ./core/...`, `go vet ./core/...`, `gofmt -l core`, `go test -run '^$' -bench RoundTrip -benchtime 2000x ./core/bridgehost/`. The race detector needs cgo: it runs in CI.
