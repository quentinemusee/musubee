# ADR 0052 — Pinned conversations: Matrix's favourite tag, listed first by the core

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.46
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

The inbox lists conversations by activity, in pages of 50 (ADR 0020, API 1.3). Users of every messenger keep a few conversations at the top: Telegram, Signal and WhatsApp call it pinning; Matrix clients call it favourites. A pin must survive restarts. It should follow the network's own pin where the bridge supports it. And it must lead the inbox even when the conversation is older than the first page.

Sources (read 2026-11-03), mautrix-go v0.31.0:
- `bridgev2/networkinterface.go`: `TagHandlingNetworkAPI`, `MatrixRoomTag`;
- `bridgev2/portal.go`: `handleMatrixAccountData` for `event.AccountDataRoomTags`; `updateUserLocalInfo`, which calls the double puppet's `TagRoom` with `UserLocalPortalInfo.Tag`;
- `event/accountdata.go`: `TagEventContent`, `RoomTagFavourite`.

## Options considered

### Where the pin is kept

#### Option A — The favourite tag in the user's room account data (chosen)
This is Matrix's own representation, which bridgev2 already maps to the networks both ways. The local server already keeps `m.tag` for the double puppet's `TagRoom`. Other tags (low priority) are kept as they are.

#### Option B — A flag of the core's own
- Cons: no way to the networks; a second truth.

### How the list puts pins first

#### Option C — The first page starts with every pin; the pages skip them (chosen)
`conversations.list` without a cursor starts with every pinned conversation, the most recently active first. Every page, the first included, leaves pinned conversations out of the activity order. The cursor is still taken from the last room of the page before the pins were taken out, so paging stays correct. A page can be shorter than its limit and still have a next one; the interface already reads pages until the end.
- Pros: a pin of an old conversation shows at once; pins are few, so the first page grows by little.

#### Option D — Sort in the interface only
- Cons: a pinned conversation older than the loaded pages would not show until the user scrolls to it.

## Decision

**Options A and C**, API 1.24: `conversations.pin` (`conversation_id`, `pinned`), and `Conversation.pinned`.

- The local server publishes a pin's change as the room's read state update, like a mute (ADR 0050). This holds for a pin set here and for one set by the network for the user. A ghost's tags are its own.
- The interface:
  - the store's order is pinned first, then by activity;
  - "Pin" in the thread's header is a toggle (`aria-pressed`);
  - the inbox entry's description says "Pinned";
  - a person's entry is pinned when one of its conversations is.

## Consequences

- No limit on the number of pins: the first page holds them all.
- Reordering pins among themselves is not offered: they follow activity.
- Networks whose connectors do not handle tags (echo, Signal for now) keep the pin in Musubee only.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The local server keeps pins here or from the network, keeps other tags, publishes changes once, and lists pinned rooms | **implemented, unverified** | `core/localmatrix/pin_test.go` |
| The list starts with a pin on its first page and never repeats it, page by page | **implemented, unverified** | `core/embedded/pin_test.go` |
| The store pins at once, keeps pins above newer activity, and goes back on a refusal | **implemented, unverified** | `ui/src/store.test.ts` |
| The desktop app keeps a pinned conversation at the top after a restart | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "a pinned conversation stays at the top" |
| Telegram's pins follow Musubee's, and the other way | **unknown** (needs a Telegram account) | |
