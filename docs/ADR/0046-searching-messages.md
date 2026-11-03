# ADR 0046 — Searching messages: LIKE over the local storage, checked against the latest text

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.32
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

The thread is virtualized (ADR 0020): the browser's find-in-page sees only the rendered rows, and T2.4 noted that conversation search had to go to the core. The messages are on the device, in the local server's `local_event` table, with their text in the event's JSON content and edits as separate events (ADR 0034).

Sources: SQLite, "The LIKE, GLOB, REGEXP, MATCH, and extract operators" (`LIKE … ESCAPE`, case-insensitive for ASCII only) and "The FTS5 Extension"; mautrix-go v0.31.0 `event.MessageEventContent` (`body`, `m.new_content`).

## Options considered

### Option A — LIKE over the stored text and the edits' text, checked against the latest text (chosen)
- Pros: no schema change, no index to keep in step with edits and redactions (a redaction already empties the content, ADR 0034); both SQLite drivers (pure Go and cgo) have it.
- Cons: a full scan of the messages for each search; case folding for ASCII letters only.

### Option B — An FTS5 index
- Pros: fast on large histories, word matching, ranking.
- Cons: a table to keep in step with every insert, edit and redaction; FTS5 must be compiled into both SQLite drivers (to check); a deleted text must leave the index too.

## Decision

**Option A, until a history is large enough to need B** (to measure on a low-end phone).

- **API 1.17**: `messages.search` (`query` 1–200 characters, `conversation_id`, `before`, `limit`) → `messages` (newest first), `before`.
- **Local server**: `SearchMessages` matches `body` and the edits' `m.new_content.body` with `LIKE … ESCAPE '\'`, the query's `%`, `_` and `\` taken literally; never deleted messages.
- **Core**: keeps the messages whose latest text (as the interface shows it: an edited text, a media caption) still contains the query, with the same ASCII-only case folding; never logs the query.
- **Interface**: a "Search messages" field in the sidebar; while it holds a query (300 ms after typing stops), the sidebar lists the messages found (conversation, time, sender and text) instead of the inbox, with "More results"; a result opens its conversation; Escape or clearing the field brings the inbox back. A newer search's results always win over an older one's late answer.

## Consequences

- A result opens its conversation at its newest message, not at the message found: jumping to a message needs the thread to read the history around it, a later task.
- File names of media without a caption are not found (the caption is the text searched).
- "É" does not find "é": SQLite's LIKE folds ASCII only.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| Found in one conversation or all, newest first, by the latest text, any ASCII case, in pages; never deleted; `%` taken literally; bad queries refused | **implemented, unverified** | `core/embedded/search_test.go` `TestSearchMessages` |
| The latest search wins; more pages; an empty query ends the search | **implemented, unverified** | `ui/src/store.test.ts` |
| The desktop app finds a message in another conversation and opens it; Escape brings the inbox back | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "search finds a message" |
