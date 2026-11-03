# ADR 0065 — Starred messages: the user's room account data, listed across conversations

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.66
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

People keep messages: an address, a code, a plan. Each network has its own way (Telegram's "Saved Messages", Signal's "Note to Self", WhatsApp's stars), and none of them reaches the others. Musubee can keep them together.

## Options considered

### Where the stars are kept

#### Option A — Per room, in the user's room account data (chosen)
Like the mute, pin, draft and archive (ADR 0050, 0052, 0057, 0060). Each room keeps a map from the starred event's ID to when it was starred. The list across rooms reads every room's map, a handful of rows. It is deleted with the room. No schema migration is needed.

#### Option B — A table of its own
- Pros: the list is one ordered query.
- Cons: a migration for a list of a few hundred entries at most.

### Whether the networks learn it

No. Telegram's saved messages are a chat of their own (forwarding there, ADR 0054, does it), and the others have no equivalent bridgev2 handles.

## Decision

**Option A**, API 1.36:
- `messages.star` returns the message as it now is;
- `Message.starred`;
- `messages.starred` lists up to 500 messages, the most recently starred first, without those whose conversation or event is gone.

The interface:
- "Star" and "Unstar" in a message's menu, and a star next to its time;
- "Starred" above the inbox lists them like search results, and a click opens the conversation at the message (ADR 0046).

## Consequences

- Stars stay on the device; they are not synced.
- Deleting a message leaves its star until the list leaves it out; a deleted message shows as deleted.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| Stars kept per room, listed newest first, unstarred | **implemented, unverified** | `core/localmatrix/starred_test.go` |
| `messages.star`, `messages.starred` and `Message.starred` in `messages.list` | **implemented, unverified** | `core/embedded/starred_test.go` |
| The store stars at once and keeps the list in step | **implemented, unverified** | `ui/src/store.test.ts` |
| The desktop app stars a message, lists it, and opens it | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "a starred message is listed" |
