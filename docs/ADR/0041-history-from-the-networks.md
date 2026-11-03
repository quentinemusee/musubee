# ADR 0041 — History from the networks: batches before the stored messages, older history only on request

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.25
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

E2.3 asks for the history of conversations. Until T2.25, bridgev2's backfill was off (`bridgehost`, no `Backfill` in the bridge configuration): a new Telegram login showed its conversations without a message until someone wrote, and nothing from before that ever came. The interface already pages through what is on the device (ADR 0020, ADR 0021).

bridgev2 brings history in three ways (mautrix-go v0.31.0, `bridgev2/portalbackfill.go`, `backfillqueue.go`):
- **forward**, when a portal's room is created (up to `MaxInitialMessages`) or when a connection catches up (`MaxCatchupMessages`);
- **backward**, by tasks (`database.BackfillTask`, one per portal, with the network's cursor), run by a background queue or by hand (`ManualBackfill`);
- through the Matrix side either in one batch (`MatrixConnector.BatchSend`, Beeper's batch send API, when `MatrixCapabilities.BatchSending`) or message by message (`sendLegacyBackfill`).

Message by message, history lands after the newest messages: the local server orders a room's events by `stream_order`, the order they were stored in (ADR 0009). Backward history needs a way to go before.

Sources: mautrix-go v0.31.0 `bridgev2/portalbackfill.go` (`doForwardBackfill`, `doBackwardsBackfill`, `sendBackfill`, `sendBatch`, `cutoffMessages`), `bridgev2/backfillqueue.go` (`WakeupBackfillQueue`, `ManualBackfill.Do`, `doBackfillTask`), `bridgev2/database/backfillqueue.go` (`EnsureExists`, `GetNextForPortal`), `bridgev2/bridgeconfig/backfill.go`, `requests.go` (`ReqBeeperBatchSend`: `Forward`, `ForwardIfNoMessages`, `MarkReadBy`); SQLite, "ROWIDs and the INTEGER PRIMARY KEY" and "AUTOINCREMENT" (an explicit rowid, negative ones included, is accepted).

## Options considered

### Where older history goes in the local storage

#### Option A — Negative stream orders below every room's (chosen)
A backward batch takes `n` orders below the lowest stored order of all rooms (and below 0, which means "no order"). Each room's older batch is then below its earlier ones; nothing is renumbered; queries ordered by `stream_order` (messages, cursors, unread counts, the last message) stay as they were.
- Cons: cursors may be negative (they are opaque to the interface); order 0 stays reserved.

#### Option B — A separate ordering column (topological order)
- Cons: every query of the local server would change, and the schema; nothing gained over Option A for a single writer.

### When older history comes

#### Option C — Only when the user scrolls to it (chosen)
`messages.fetchOlder` runs one backward batch for the conversation through `ManualBackfill.Do`, in the request, then returns what is older than the oldest message the device had. The background queue stays off.
- Pros: a login does not walk every conversation's history on the network (rate limits: Telegram's `FLOOD_WAIT`; a phone's data and battery); the user gets what they look for.
- Cons: scrolling waits for the network (bounded by the request's 30-second limit).

#### Option D — The background queue
- Cons: the opposite of each pro above, on every login.

## Decision

**Options A and C.**

- **Local server** (`core/localmatrix`): `BatchSend` stores a batch after the stored events (forward, published like any event), or before every stored message (backward, not published: the interface reads it back from the command's answer); a backward batch goes forward in a room without messages when bridgev2 asks so (`ForwardIfNoMessages`); the senders join first; an event already stored (bridgev2's deterministic IDs) is skipped. History the user asked for is never unread: without a read position, the room gets one past the batch. `BatchSending` is advertised.
- **Bridge configuration** (`core/bridgehost`): backfill on; the latest 50 messages of a new conversation, up to 500 on a catch-up; messages older than 24 hours come read (`UnreadHoursThreshold`); the queue off, batches of 100, no limit on the number of batches asked by hand.
- **API 1.13**: `messages.fetchOlder` (`conversation_id`, `limit` up to 200) → `messages` (oldest first), `before` (a cursor when the network gave more than the limit), `end` (the network has no more, or gives no history). It ensures the conversation has a task (conversations made before T2.25 have none), runs one batch, and fails with `network` when the account is not connected.
- **Echo network**: ContactInstant has three pages of twenty older messages ("Old message 0" to "Old message 59"), the user and the contact in turn; other conversations have none.
- **Interface**: the history of a conversation goes from cursors on the device to `"network"`, then `"start"` once the network has no more, gives nothing, or the core does not know the command (an older core). Scrolling to the top asks for the next page.

## Consequences

- Telegram logins now bring the latest 50 messages of each conversation at once: more requests at login than before. The Telegram journeys on CI use bots, which cannot read history (the connector's errors are logged, the login goes on): **unknown** whether this adds `FLOOD_WAIT` to the CI runs; to watch in the test session.
- Signal gives no history (a linked device gets only new messages), and the Matrix connector has no `FetchMessages` yet: their conversations end at once.
- Offline behaviour (sending while disconnected queues nothing yet; the message fails) is left to a later task of E2.3.
- A message older than 24 hours from a forward batch comes read; a newer one counts as unread, as it would on the network's own app after a reinstall.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| Backward batches go before every stored message, each below the earlier ones, even after another room's; forward ones after; duplicates skipped; cursors below 0 page; history is not unread | **implemented, unverified** | `core/localmatrix/history_test.go` `TestBatchSendHistory` |
| A backward batch in an empty room goes forward, published, and read | **implemented, unverified** | `TestBatchSendForwardIfNoMessages` |
| The core pages through the echo network's history, with a cursor beyond the limit, to its end; the whole history in order; one unread message only | **implemented, unverified** | `core/embedded/history_test.go` `TestFetchOlder` |
| A conversation without history ends at once; a bad limit is refused | **implemented, unverified** | `TestFetchOlderWithoutHistory` |
| The contract: the new command has examples, valid for Go's and TypeScript's validators | **implemented, unverified** | `core/api` `TestExamplesCoverTheSchema`; `ui/src/core-api/contract.test.ts` |
| The store asks the network after the device's history, keeps the cursor beyond a page, stops at the end, and treats an older core as the start | **implemented, unverified** | `ui/src/store.test.ts` |
| Scrolling up in the desktop app reaches the echo network's oldest message | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "scrolling up brings the network's older history" |
| Telegram's history comes the same way | **assumed** (mautrix-telegram implements `FetchMessages`) | |
