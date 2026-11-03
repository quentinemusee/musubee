# ADR 0054 — Forwarding a message: a new message of the user's, built by the core from what the device stores

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.48
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

A universal messenger should let the user pass a message from one network to another: a photo received on Telegram, sent on to a Signal contact. No network can forward to another network. Forwarding within a network (Telegram's "Forwarded from") is network-specific, and bridgev2 has no forwarding interface.

Questions: who builds the copy; what the copy holds; what it says of its origin.

## Options considered

### Who builds the copy

#### Option A — The core, from the device's storage (chosen)
`messages.forward` reads the message as the user sees it, with its latest edit (ADR 0034). It then sends its text through `messages.send`'s path. For media, it reads the bytes from the local storage and sends them through `messages.sendMedia`'s path, which now takes bytes the core holds as well as an upload. Every check of those commands applies, and the destination's network takes or refuses the kind and the size.
- Pros: works between any two networks; the media never goes through the interface.

#### Option B — The interface reads the media and uploads it again
- Cons: up to 100 MiB through the bridge between the interface and the core, twice, for nothing.

### What the copy says of its origin

#### Option C — Nothing (chosen)
The copy is a new message of the user's. It answers nothing, and does not say "Forwarded from Alice": networks have no common way to say it, and a text prefix would change the message. Telegram's own forward, with its header, would need a connector-specific path; that is a later option.

## Decision

**Options A and C**, API 1.26: `messages.forward` (`message_id`, `conversation_id`).
- The copy keeps the text, or the media with its caption, its dimensions and duration, and whether it is a GIF.
- It fails:
  - for a deleted message;
  - for media that is not on this device;
  - where the destination's network refuses the kind, the caption or the size.
- The interface: "Forward…" in a message's menu, for messages on their network with text or media. It opens a dialog that lists the inbox's conversations, filtered by name (ignoring case and accents, as the merge panel does), and says where the copy went or why it could not.

## Consequences

- Forwarding can move a message from an end-to-end encrypted conversation into one that a bridge host or the network can read. The dialog says so next to each such destination ("Telegram can read it there", from the destination's privacy, ADR 0039).
- Media that the device has not stored (history beyond what was downloaded) cannot be forwarded.
- Forwarding several messages at once is a later task.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| A text, a photo with its caption (same bytes) and a GIF (still animated) are forwarded; unknown messages and conversations fail | **implemented, unverified** | `core/embedded/forward_test.go` |
| Which messages offer forwarding | **implemented, unverified** | `ui/src/message.test.ts` |
| The warning for a destination that others can read | **implemented, unverified** | `ui/src/privacy.test.ts` |
| The desktop app forwards a message from one echo conversation to another | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "the user forwards a message" |
