# ADR 0050 — Muting a conversation: kept by the local server, told to the network, followed by every notifier

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.44
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

The app notifies the user of new messages on Android (ADR 0033) and on the desktop (ADR 0047). A busy group then becomes noise. Every messenger lets the user mute a conversation for a while or for good, and the networks keep that choice: Telegram has notify settings per chat, and Matrix clients keep a push rule per room. Musubee needs the same, without breaking what the user set in the network's own apps.

Three questions: where the mute is kept; how the networks learn it, both ways; who applies it.

Sources (read 2026-11-03):
- mautrix-go v0.31.0:
  - `bridgev2/networkinterface.go`: `MuteHandlingNetworkAPI`, `MatrixMute`;
  - `bridgev2/portal.go`: `handleMatrixAccountData` for `event.AccountDataBeeperMute`; `updateUserLocalInfo`, which calls the double puppet's `MuteRoom` with `UserLocalPortalInfo.MutedUntil`; `Unmuted`, a date in 2024;
  - `event/accountdata.go`: `BeeperMuteEventContent` (`muted_until`, -1 forever) and `MutedForever`.
- mautrix-telegram v0.2609.0: `pkg/connector/handlematrix.go` (`HandleMute` sets the chat's notify settings) and `pkg/connector/chatinfo.go` (the chat's `MuteUntil` becomes `UserLocal.MutedUntil`).

## Options considered

### Where the mute is kept

#### Option A — The user's room account data in the local server (chosen)
The local server already keeps the read marker and the "marked as unread" flag there (ADR 0023). The bridge's double puppet writes the network's mute there through `MuteRoom`. The core reads it with the room's read state.
- Pros: one place for the user's own state of a room; the network's mute and Musubee's are the same value; no new table.

#### Option B — A table of the core's own
- Cons: two values to reconcile with what the network says.

### How the networks learn it

#### Option C — com.beeper.mute account data to the bridge (chosen)
`conversations.mute` stores the mute and queues the `com.beeper.mute` event that bridgev2 hands to connectors implementing `MuteHandlingNetworkAPI`. Telegram's connector implements it: Telegram's other apps go quiet too. Connectors without it keep the mute in Musubee only. The other way, a mute set in the network's app arrives through `MuteRoom`, as a read receipt does.

### Who applies it

#### Option D — Each notifier, from `Conversation.muted_until_ms` (chosen)
- The Android service already follows `conversation.updated` (ADR 0033): it skips muted conversations, and muting removes the notification shown.
- The desktop main process learns each conversation's mute from the core's events and from its answers to the interface, which pass through it. It keeps names, kinds and mutes only: never a message.
- A muted conversation still counts its unread messages. The interface greys its badge and says "Muted".

The core sends no event when a timed mute ends. Each notifier compares the end with its own clock instead.

## Decision

**Options A, C and D**, API 1.23:
- `conversations.mute` takes a conversation and `until_ms`: a time, -1 until unmuted, or 0 to unmute;
- `Conversation.muted_until_ms` carries the mute, and is omitted once the mute has ended;
- the interface:
  - "Mute…" in the thread's header offers 1 hour, 8 hours, 1 week, or "Until I unmute it";
  - a muted thread says "Muted until 14:30" and offers "Unmute".

Since the main process now knows the conversations' names, a group's desktop notification is titled "Alice in Book club" (an item left by ADR 0047).

## Consequences

- Muting on one Musubee device reaches the user's other Musubee devices only through the network: through Telegram, but not through the echo network or Signal, whose connectors do not handle mutes.
- A conversation muted while a notification of it is shown loses that notification on Android. On the desktop, Electron's notification stays until it is clicked or replaced.
- A person's entry in the inbox is muted only when all of its conversations are.
- Since T2.50, a message that mentions the user gets through a mute (ADR 0055).

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The local server keeps a mute set here or by the network, publishes a change once, ignores a ghost's mute, and reads a past date as no mute | **implemented, unverified** | `core/localmatrix/mute_test.go` |
| `conversations.mute` reaches `Conversation.muted_until_ms`, refuses bad values and unknown conversations | **implemented, unverified** | `core/embedded/mute_test.go` |
| The desktop skips a muted conversation's notifications, and names the group | **implemented, unverified** | `apps/desktop/test/notification-rules.test.ts` |
| Android skips a muted conversation's notifications, and removes the one shown | **implemented, unverified** | `MessageNotificationsTest.aMutedConversationStaysQuiet` |
| The store's mute, back on a refusal; labels and choices | **implemented, unverified** | `ui/src/store.test.ts`, `ui/src/mute.test.ts` |
| The desktop app mutes, keeps the mute after a restart, and unmutes | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "the user mutes a conversation" |
| Telegram's other apps follow a mute set in Musubee | **unknown** (needs a Telegram account; the test bots' chats may not have notify settings) | |
