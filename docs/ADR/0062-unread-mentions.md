# ADR 0062 — Unread mentions: counted by the local server from m.mentions

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.60
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

ADR 0055 marks each message that mentions the user. The inbox needs the count of unread ones per conversation. That count is what tells the user, in a muted group with hundreds of unread messages, that one of them is a question to them.

## Options considered

#### Option A — The local server, with the unread count (chosen)
SQL picks the unread messages that carry `m.mentions`, with the same conditions as the unread count (ADR 0023). Go then checks whether they name the user or the room, up to 1000 like the unread count.
- Pros: exact, with no JSON functions in SQL (the two SQLite drivers, ADR 0011, need not agree on them); the extra query runs only when something is unread.
- Cons: a mention added by an edit is not counted (the original's content is checked).

#### Option B — A column filled when events are stored
- Cons: a schema change and a migration for a count that the query gives cheaply.

## Decision

**Option A**, API 1.34: `Conversation.unread_mentions`. The inbox shows an "@" before the unread badge, said as "1 mention of you".

## Consequences

- Reading the conversation clears it with the unread count.
- Edits that add a mention are not counted; edits that remove one still count until read.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| Mentions of the user or the room are counted, others not, and reading clears them | **implemented, unverified** | `core/localmatrix/mentions_test.go` |
| The echo's mention reaches `unread_mentions` | **implemented, unverified** | `core/embedded/mentions_test.go` |
