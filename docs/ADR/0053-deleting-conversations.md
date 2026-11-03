# ADR 0053 — Deleting a conversation: bridgev2's delete chat, confirmed by the room's deletion

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.47
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

Users want to remove a conversation they no longer need, and its history with it. The history lives in two places: the local server's database on the device, and the network. A deletion that clears the device only would come back at the next history sync (ADR 0041). One that clears the network only would leave the device's copy.

Sources (read 2026-11-03):
- mautrix-go v0.31.0:
  - `bridgev2/networkinterface.go`: `DeleteChatHandlingNetworkAPI`, `MatrixDeleteChat`;
  - `bridgev2/portal.go`: `handleMatrixDeleteChat` calls the connector, then deletes the portal and the bot deletes the room; it sends no message status that the device would track;
  - `event/beeper.go`: `BeeperChatDeleteEventContent`;
  - `event/capabilities.go`: `RoomFeatures.DeleteChat`.
- mautrix-telegram v0.2609.0, `pkg/connector/handlematrix.go` (`HandleMatrixDeleteChat`): a private chat's history is cleared for the user (`JustClear`). A group or channel is refused unless deleted for everyone.

## Options considered

### How the deletion is made

#### Option A — com.beeper.delete_chat to the bridge, then wait for the room to go (chosen)
The local server queues the event to the room's bridge, as it does for read receipts and mutes (ADR 0023, 0050). bridgev2 asks the connector, then deletes the portal and the room. Deleting the room deletes its events, media and account data (`ON DELETE CASCADE`). The local server publishes `RoomDeleted` for every deleted room, including a chat deleted on the network.

bridgev2 says nothing when the connector refuses. The core therefore subscribes before asking, then waits for the room's deletion. After 30 seconds, it checks whether the room still exists, and fails if it does.

#### Option B — Delete the device's copy only
- Cons: the network's history comes back with the next sync; the user's other apps keep it.

### Which conversations

#### Option C — Direct conversations only, "for me" (chosen)
A direct conversation is cleared for the user; the other person keeps their copy. Networks delete groups for everyone or let the user leave: that is a different action, with different consequences, and a later task. "Delete for everyone" is not offered either.

## Decision

**Options A and C**, API 1.25:
- `conversations.delete`;
- the `conversation.deleted` event;
- `ConversationActions.delete_chat`, from the room features, for direct conversations;
- the echo network offers it and deletes nothing on its side;
- the interface:
  - "Delete…" in the thread's header opens a dialog that says what goes and where ("as Telegram allows, from your other apps"), and that it cannot be undone;
  - the dialog closes once the network deleted the conversation, or says why it could not;
  - the store forgets the conversation and its messages, and leaves the thread if it was open;
- the Android and desktop notifications of a deleted conversation go away.

## Consequences

- A person keeps their link to a deleted conversation (ADR 0017); it lists only the conversations still on the device, so the link shows again if the chat comes back.
- The echo network creates its contacts' conversations again at its next login, not at a restart.
- Leaving groups came in T2.51 (ADR 0056); deleting groups and deleting for everyone are later tasks.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The echo network deletes a conversation: event, list and messages | **implemented, unverified** | `core/embedded/delete_chat_test.go` |
| The store forgets a conversation deleted here or on the network, and leaves it if open | **implemented, unverified** | `ui/src/store.test.ts` |
| The desktop app asks, cancels, then deletes | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "the user deletes a conversation after confirming" |
| Notifications of a deleted conversation go | **implemented, unverified** | `MessageNotificationsTest.aDeletedConversationLosesItsNotification`, `apps/desktop/test/notification-rules.test.ts` |
| Telegram clears a private chat for the user | **unknown** (needs a Telegram account) | |
