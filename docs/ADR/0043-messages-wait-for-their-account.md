# ADR 0043 — Messages sent without network wait on the device until their account connects

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.27
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

E2.3 asks for offline behaviour. Reading offline works since T2.5 (ADR 0021): the conversations and messages are on the device. Sending did not: `messages.send` handed the message to its bridge at once, and a disconnected connector failed it (the echo network with "the echo network client is disconnected"; Telegram and Signal with their own errors). The user had to notice and write it again. Messaging apps keep such a message and send it when they can.

Where the message waits matters: bridgev2 queues Matrix events per portal in memory (`PortalEventBuffer`) and hands them to the connector at once; a connector's failure while disconnected is final for that event. Nothing in bridgev2 holds an event until a login connects.

Sources: mautrix-go v0.31.0 `bridgev2/portal.go` (`queueEvent`, `handleMatrixMessage`), `bridgev2/status` (`BridgeStateEvent`, `StateConnected`); the local server's message statuses (ADR 0009).

## Options considered

### Option A — The core holds the message in the local server until the account connects (chosen)
The message is stored as usual, with a pending status marked "queued", and not handed to the bridge. When the account's bridge state turns connected, the core hands the held messages to their bridges, the oldest first.
- Pros: survives a restart (the hold is in the database); the order of a room's messages is kept; the interface sees one more status, nothing else.
- Cons: "connected" is the bridge state the connector reports; a connector that says connected while it cannot send still fails the message, as before.

### Option B — The interface keeps unsent messages and sends them again
- Cons: lost when the app closes; every shell would need it; duplicates if the first attempt reached the network after all.

### Option C — Retry failed messages
- Cons: a refused message (a blocked contact) would be retried for nothing; the network may have received a "failed" message.

## Decision

**Option A.**

- **API 1.14**: `MessageStatus` gains `queued`: "a message from the user that waits on this device for its account to connect, and goes then". `messages.send` and `messages.sendMedia` return it while the conversation's account is not connected (any state but `connected`: connecting, transient disconnection, bad credentials, logged out).
- **Local server** (`core/localmatrix`): `QueueMessage` stores a message pending with the reason `app.musubee.queued`; `Queued` lists the held messages in stream order; `Release` takes one out of the hold atomically (an `UPDATE` that matches only a held message) and hands it to the bridge, so that two releases never send it twice; a held message deleted meanwhile is failed, not sent.
- **Core** (`core/embedded/outbox.go`): a conversation's account is the login of its portal and its last bridge state; a room whose account the core cannot find yet counts as connected (as before). Every `account.updated` to `connected` releases the held messages in the background, one release at a time; a message sent once the account is back first releases the room's held ones, to keep the order. The log names no message nor room.
- **Interface**: the status reads "Waiting for the network"; a waiting message offers no action (it is not on the network yet).

## Consequences

- A message written offline goes when the account reconnects, even days later, with the time it was written: a network shows it at the time it receives it. A later task can offer to cancel a waiting message.
- An account that never connects again (logged out) keeps its messages waiting; logging out removes them with the conversations.
- Reactions, edits and deletions are not held: they still fail while offline (they need the network's answer, ADR 0035).

## Verification

| Claim | Status | Evidence |
|---|---|---|
| Messages sent while the account is away are queued, listed as queued, and go in order once it connects; then messages go at once | **implemented, unverified** | `core/embedded/outbox_test.go` `TestMessagesWaitForTheirAccount` |
| An offline account's message waits | **implemented, unverified** | `core/embedded/embedded_test.go` `TestOfflineLoginKeepsItsConversations` |
| The contract: the new status has an example, valid for Go's and TypeScript's validators | **implemented, unverified** | `core/api` `TestExamplesCoverTheSchema`; `ui/src/core-api/contract.test.ts` |
| The desktop app shows a waiting message, and still after a restart | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "an account without network keeps its conversations and says so" |
| Telegram and Signal report `connected` only when they can send | **assumed** | |
