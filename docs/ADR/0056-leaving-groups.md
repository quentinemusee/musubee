# ADR 0056 — Leaving a group: the user's leave event to the bridge, confirmed by its status

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.51
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

ADR 0053 lets the user delete a direct conversation, and leaves groups aside: networks delete a group for everyone, or let a member leave it (mautrix-telegram refuses deleting a group for the user alone). Leaving is the user's way out of a group.

Sources (read 2026-11-03), mautrix-go v0.31.0:
- `bridgev2/queue.go`: `QueueMatrixEvent` hands a portal's events, state events included, to the portal; `EventHandlingResult.WithMSS`;
- `bridgev2/portal.go`, `handleMatrixMembership`:
  - drops the user's own leave unless `BridgeMatrixLeave` is set;
  - otherwise calls `MembershipHandlingNetworkAPI.HandleMatrixMembership` with `Leave` (join to leave, self);
  - returns a result with a message status, success or failure;
  - the previous membership comes from `Unsigned.PrevContent`;
- `bridgev2/networkinterface.go`: `MembershipChangeType`, `Leave`;
- `bridgev2/portal.go`, `Portal.Delete`.

## Options considered

### How the network learns it

#### Option A — The user's leave as a member event to the bridge, waited for by its status (chosen)
The local server queues the user's leave (with the previous membership, join) to the room's bridge without storing it. bridgev2 hands it to the connector and reports the answer as the event's message status, which the local server stores and publishes like a message's (ADR 0034). The core waits for that status. On success, it deletes the portal, so that being added to the group again makes a new room rather than pointing at a deleted one. Then it deletes the room.
- Pros: the network's refusal reaches the user with its reason.

#### Option B — Keep the room, marked as left
- Cons: a conversation that can no longer be written to, in the inbox; nothing in the product needs it yet.

### BridgeMatrixLeave

It is now on. The local server sends the user's leave events only from this command, so no other action of the user's becomes a leave on the network.

## Decision

**Option A**, API 1.29:
- `conversations.leave`, which fails if the network refuses or does not answer within 30 seconds;
- `ConversationActions.leave`, for groups whose connector handles memberships;
- `conversation.deleted` once the group is gone;
- the echo network accepts the user leaving its group;
- the interface: "Leave…" in a group's header, with the delete dialog worded for leaving: "You leave the group on Telegram, and its messages go from Musubee. The other members keep theirs. To come back, someone must add you again."

The status of an event that is not stored no longer fails the core's event loop: it is the answer this command waits for.

## Consequences

- Deleting a group for everyone, for its owner, is still not offered.
- The echo group comes back at the echo account's next login.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The echo group is left and removed; a direct conversation cannot be left | **implemented, unverified** | `core/embedded/leave_test.go` |
| The desktop app offers "Leave…" for a group, not "Delete…", and leaves it | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "the user leaves a group after confirming" |
| Telegram leaves a group or channel | **assumed** (mautrix-telegram's `HandleMatrixMembership` leaves a channel, or deletes the user from a basic group; needs a Telegram account) | |
