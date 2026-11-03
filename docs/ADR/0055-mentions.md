# ADR 0055 — Mentions: the network's own marking, shown in the thread and let through a mute

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.50
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

Muting a busy group (ADR 0050) should not hide a question addressed to the user. Every network lets members mention each other, and most notify the person mentioned even in a muted chat.

Questions: how the core knows that a message mentions the user; what the apps do with it.

Sources (read 2026-11-03):
- the Matrix client-server specification, "User and room mentions" (`m.mentions`, `user_ids`, `room`);
- mautrix-go v0.31.0, `event/message.go` (`Mentions`);
- mautrix-telegram v0.2609.0:
  - `pkg/connector/telegramfmt/convert.go` turns Telegram's mention entities into `content.Mentions.UserIDs`;
  - `pkg/connector/client.go` (`GetUserInfoByID`) gives the logged-in user's own Matrix ID for a mention of them.

## Options considered

### How the core knows

#### Option A — The network's marking, Matrix's m.mentions (chosen)
A message mentions the user when its latest content lists the user's Matrix ID in `m.mentions.user_ids`, or mentions the whole room. Connectors fill this from the network's own entities.
- Pros: exact; no guessing from names, which are not unique and change.

#### Option B — Search the text for the user's name
- Cons: false positives ("Ada" in "Adam"), names that differ per network, and no help where the network renders mentions as plain IDs.

### What the apps do

- The thread outlines the message with the accent color and says "Mentions you" first to screen readers.
- A mention gets through a mute, alone: Android posts the mentioning lines only, and the desktop announces it. A conversation that is not muted notifies as before.

## Decision

**Option A**, API 1.28: `Message.mentions_me`, for messages of others. It is computed from the latest version of the message, so an edit that adds or removes the mention counts. The echo contact answers `/mention text` with the text, mentioning the user, so that tests can produce one.

## Consequences

- A connector that does not fill `m.mentions` gives no mentions. Signal's and the Matrix connector's behaviour is to be checked.
- No separate "mentions only" notification level yet: a muted conversation with mentions let through is close to it.
- The inbox does not show an "@" for unread mentions yet.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The echo contact's mention reaches `mentions_me`; plain messages do not | **implemented, unverified** | `core/embedded/mentions_test.go` |
| A mention gets through a mute on the desktop and on Android | **implemented, unverified** | `apps/desktop/test/notification-rules.test.ts`, `MessageNotificationsTest.aMutedConversationStaysQuiet` |
| The thread shows a mention | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "a message that mentions the user stands out" |
| Telegram's mentions of the user arrive as `mentions_me` | **assumed** (from mautrix-telegram's code; needs a Telegram account) | |
