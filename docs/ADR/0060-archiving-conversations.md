# ADR 0060 — Archiving conversations: on the device, brought back by new messages unless muted

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.58
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

Users keep conversations they are done with but do not want to delete (ADR 0053) or leave (ADR 0056). Telegram, WhatsApp and Signal archive them: they leave the main list and wait in an "Archived" list.

mautrix-go v0.31.0 has `RoomFeatures.Archive` (`event/capabilities.go`), but no interface to hand an archiving to a connector, and mautrix-telegram has none either (its folders are not bridged). Archiving can therefore not reach the networks yet.

## Options considered

### Where the archiving is kept

#### Option A — The user's room account data in the local server (chosen)
It sits with the mute, the pin and the draft (ADR 0050, 0052, 0057), and is deleted with the room. It is kept on the device only.

#### Option B — Matrix's low priority tag
- Cons: bridgev2 would hand the tag to connectors that handle tags (ADR 0052), which read only the favourite tag today but might map low priority to something else.

### When an archived conversation comes back

#### Option C — On a new message from someone else, unless muted (chosen)
This is Telegram's rule. Only a message written after the archiving counts: history that arrives later does not. A muted conversation stays archived, which is how users silence a busy group for good.

#### Option D — Never by itself (WhatsApp's option)
- Cons: an answer the user waits for would go unseen.

## Decision

**Options A and C**, API 1.31:
- `conversations.archive` (`archived`) and `Conversation.archived`;
- the core unarchives on a message as above, then sends the conversation again;
- the interface:
  - the inbox lists archived entries apart, behind "Archived (n)"; a person's entry is archived when all of its conversations are;
  - the thread offers "Archive" and "Unarchive".

## Consequences

- Archiving does not reach the user's other apps.
- Notifications follow the mute, not the archive: an archived conversation that is not muted notifies, and comes back.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| Archiving, an answer bringing the conversation back, a muted one staying archived | **implemented, unverified** | `core/embedded/archive_test.go` |
| The store and the inbox's archived entries | **implemented, unverified** | `ui/src/store.test.ts`, `ui/src/inbox.test.ts` |
| The desktop app archives, shows the archive, and unarchives | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "an archived conversation leaves the inbox" |
