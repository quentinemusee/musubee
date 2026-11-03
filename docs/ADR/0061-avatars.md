# ADR 0061 — Avatars: the room's picture from bridgev2, or initials on the name's hue

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.59
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

The inbox and the thread showed names only. Networks give pictures of contacts and groups. bridgev2 uploads them to the local server's media store and sets them on the rooms. The interface needs a way to read them without any Matrix type (`CLAUDE.md` §2).

Sources (read 2026-11-03), mautrix-go v0.31.0:
- `bridgev2/ghost.go` (`UpdateAvatar` uploads the picture and sets the ghost's profile);
- `bridgev2/portal.go`:
  - `UpdateInfoFromGhost`: with `PrivateChatPortalMeta`, a direct conversation's room gets its contact's picture;
  - `updateAvatar` sets `m.room.avatar`;
- `bridgev2/networkinterface.go` (`Avatar`).

## Options considered

### Which picture

#### Option A — The room's m.room.avatar (chosen)
One rule for groups and direct conversations, since `PrivateChatPortalMeta` is on (ADR 0021). It is exposed as a media ID that `media.read` reads (ADR 0036), like a message's media.

#### Option B — The contact's profile picture for direct conversations
- Cons: a second lookup; the same picture in the end.

### Without a picture

Initials (two at most) on a light background whose hue comes from the name, with dark text: about 10:1 contrast at the chosen lightness. The same name always gets the same color.

## Decision

**Option A**, API 1.32: `Conversation.avatar_id`. The conversation is sent again when its picture changes. The echo contacts have a generated picture, so the apps and the tests show avatars without a real network.

The interface reads the picture through the media cache, shown at 40 pixels in the inbox and 32 in the thread's header. The avatar is hidden from screen readers: the name is always written next to it.

## Consequences

- A person's entry shows the picture of its most recently active conversation.
- Since API 1.33, `Message.sender_avatar_id` is the sender's current picture, from their global profile (the local server does not rewrite member events), shown small before the sender's name. The members' list (API 1.27) has no pictures yet.
- Pictures are read whole, as message media are; the cache's limit (64 MiB) bounds them.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| A direct echo conversation has its contact's picture, readable as PNG | **implemented, unverified** | `core/embedded/avatar_test.go` |
| The echo's answer carries its sender's picture; the user's own message does not | **implemented, unverified** | `core/embedded/avatar_test.go`, `TestSenderAvatar` |
| Initials and hues | **implemented, unverified** | `ui/src/avatar.test.ts` |
| Telegram's pictures show | **assumed** (bridgev2's path; needs a Telegram account) | |
