# ADR 0057 — Drafts: kept by the core with the messages, saved a moment after typing

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.52
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

T2.40 kept the text the user left unsent in a conversation's composer in the interface's memory only. Its stated reason was that unsent text stays off the disk. Closing the app, or the system ending it on Android, loses the draft, which users of every messenger expect to find again.

The messages themselves are stored on the device, in the core's database. That database is protected the same way as the rest of the app's data (the sealed columns of ADR 0019 and 0026 hold secrets, not messages). A draft is no more sensitive than the messages around it.

## Options considered

### Where a draft is kept

#### Option A — The core, as the user's room account data in the local server (chosen)
Like the read marker, the mute and the pin (ADR 0023, 0050, 0052), it is the user's own state of a room, deleted with the room. No bridge ever sees it: the local server does not hand this account data to bridges.
- Pros: one place for the user's state of a conversation; available to every shell; gone with the conversation (ADR 0053, 0056).

#### Option B — The page's storage
- Cons: a second copy of message text outside the core, kept after the conversation is deleted, and not readable by the Android service.

#### Option C — Keep T2.40's memory-only drafts
- Cons: lost at every restart.

### When it is saved

A moment after the user stops typing: one second, and only when the text changed. The interface keeps the text in memory at once, so switching conversations never waits for the core. Up to a second of typing can be lost if the app closes at once.

## Decision

**Option A**, API 1.30:
- `conversations.setDraft` (an empty text forgets the draft) and `Conversation.draft`;
- no `conversation.updated` follows a save, since the interface that wrote the draft knows it;
- the store keeps its own drafts first, and falls back to the core's.

This reverses T2.40's memory-only rule.

## Consequences

- Drafts follow the device, not the account: Matrix's own draft sync (none in the specification) and the networks' drafts (Telegram syncs them) are not used.
- The inbox does not show "Draft:" in previews yet.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The local server keeps, replaces and forgets a draft | **implemented, unverified** | `core/localmatrix/draft_test.go` |
| The core gives a draft back in the conversation list | **implemented, unverified** | `core/embedded/draft_test.go` |
| The store saves after a pause, and reads the core's draft | **implemented, unverified** | `ui/src/store.test.ts` |
| The desktop app finds a draft after a restart | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "an unsent text waits in its conversation's composer, after a restart too" |
