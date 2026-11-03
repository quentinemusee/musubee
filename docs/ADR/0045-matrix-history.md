# ADR 0045 — A Matrix room's history: paged from the homeserver on request, decrypted with the device's keys

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.29
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

T2.25 (ADR 0041) brings the networks' older history through bridgev2's backfill when the user scrolls past the oldest message on the device (`messages.fetchOlder`). The Matrix connector (ADR 0018) did not implement `BackfillingNetworkAPI`: a Matrix conversation began at the core's login. T2.28 (ADR 0044) restores an account's key backup, which only helps if older encrypted messages reach the device.

Sources: mautrix-go v0.31.0 `client.go` (`Client.Messages`, `Client.Context`), `bridgev2/networkinterface.go` (`FetchMessagesParams`, `FetchMessagesResponse`, `BackfillMessage`), `crypto/cryptohelper` (`Decrypt`); the Matrix client-server specification, "Syncing" and "GET /rooms/{roomId}/messages", "GET /rooms/{roomId}/context/{eventId}".

## Options considered

### Option A — Backward pages from the homeserver, on request (chosen)
`FetchMessages` (backward) asks `/messages` for up to 100 message and encrypted events before the oldest bridged message (`/context` gives the token before it; later pages continue from the previous page's `end`), decrypts them, and gives them oldest first. Forward fetches give nothing: the sync covers what happens after the login, and the sync's first timeline may already hold the latest messages, which bridgev2 recognises by their event IDs.
- Pros: the same path as every network (ADR 0041); the restored keys decrypt the history.
- Cons: edits, reactions and redactions in older history are not applied (the message shows as first sent; a redacted one is left out).

### Option B — The initial sync's timeline only
- Cons: a few messages per room, no paging.

## Decision

**Option A.**

- `core/connector/matrix/history.go`: `FetchMessages`; a message the device has no key for becomes the same "could not be decrypted" notice as a live one; media is copied to the device as for live messages (ADR 0036), with the bridge bot's intent; the sender's ghost is made first; replies keep their target. Rooms declare `CanBackfill`.
- History comes only on request (ADR 0041); bridgev2 marks the batch's messages read.

## Consequences

- After verifying with a recovery key (ADR 0044), scrolling up decrypts the history that the restored backup has keys for. Messages that the sync brought before the verification stay as "could not be decrypted": they are already on the device.
- A room's history pages as far back as the homeserver serves it to the user (its history visibility setting).
- Since T2.42, a page's edits (the sender's latest) and reactions are applied to its messages: the message shows its latest text, without the "Edited" mark, and its reactions (keyed by their event IDs, as live ones); redacted events are left out. Changes whose message is on another page are not applied.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| History from before the device comes back once each, in order, as undecryptable messages from their sender | **implemented, unverified** (CI only: Synapse) | `infra/matrixtest/history_integration_test.go` `TestMatrixHistory` |
| Restored keys decrypt the history | **assumed** (the same `Decrypt` as live messages; not in a test yet) | |
