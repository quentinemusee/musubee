# ADR 0034 — Changes to messages: relations kept in the local server, a deletion erases the edits too, received only

- **Status**: accepted
- **Date**: 2026-10-20
- **Task**: T2.18
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

Until T2.18 the core showed a message as it was first sent. Networks change messages afterwards, and the bridgev2 connectors already turn those changes into Matrix events in the core's local server (`core/localmatrix`, ADR 0009):

| Network change | What bridgev2 sends to the room | What the core showed |
|---|---|---|
| An edit | `m.room.message` with `m.relates_to` `{rel_type: m.replace}`, the new content in `m.new_content`, and `"* new text"` as the body for old clients | A new message "* new text" |
| A reaction | `m.reaction` with `m.relates_to` `{rel_type: m.annotation, key: emoji}` | Nothing |
| Taking a reaction back, deleting a message | `m.room.redaction` naming the event | Nothing in the interface |
| A reply | `m.room.message` with `m.relates_to` `{m.in_reply_to}` | The answer, without what it answers |

E2.4 asks for replies, reactions and editing in the thread. Showing them is what this ADR settles; sending them from Musubee is T2.19.

Sources: the Matrix client-server specification, sections "Event replacements", "Event annotations and reactions", "Redactions" and "Rich replies"; mautrix-go v0.31.0 `bridgev2/portal.go` (`handleRemoteEdit`, `handleRemoteReaction`, `handleRemoteReactionRemove`, `handleRemoteMessageRemove`) and `event/relations.go`; [MSC3389](https://github.com/matrix-org/matrix-spec-proposals/pull/3389) (keeping `m.relates_to` through a redaction).

## Options considered

### Where to aggregate relations

#### Option A — Relation columns in `local_event`, aggregated when messages are read (chosen)
Schema version 4 adds `rel_type`, `relates_to` and `redacted_by` to `local_event`, with an index on `(relates_to, rel_type)`. `Server.Relations` reads, for a page of messages, their edits and reactions in one query.
- Pros: one source of truth, the events themselves; a redaction only has to mark rows; pagination and the conversation list filter edits out with a column instead of parsing JSON.
- Cons: every page of history costs a second query.

#### Option B — Aggregates kept up to date as events arrive (a table of reactions, the latest edit per message)
- Pros: reading is one query.
- Cons: a second copy of the same facts to keep consistent through redactions, schema upgrades and Matrix's sender rule; any bug shows a stale state that a restart does not fix.

#### Option C — Aggregate in the domain layer from all events
- Pros: no schema change.
- Cons: the history is paginated, and an edit or a reaction can come long after the page that holds its message.

### What a deletion leaves

#### Option D — Erase the message and its edits, keep their relations (chosen)
A redaction empties the content of the event it names (as before T2.18) and of every `m.replace` of it, leaving only `m.relates_to` in the edits, as MSC3389 proposes, and sets `redacted_by`. The migration erases the edits of messages deleted before version 4.
- Pros: a deleted message's text is gone from the device, edits included, which is what the person who deleted it expects; the relation still ties the erased edit to its message, so it never comes back as a message of its own.
- Cons: Matrix's current redaction algorithm (before MSC3389) removes `m.relates_to` too; ours keeps it. Nothing outside the core reads these events.

#### Option E — Erase the message only (Matrix's algorithm)
- Cons: the edits keep the deleted text in `m.new_content`, readable in the database, and come back as messages "* text" once their relation is gone.

## Decision

**Options A and D.**

- **Local server** (`core/localmatrix`):
  - `03-relations.sql` (schema 4) adds the columns, fills them from the stored events, fills `redacted_by` from the stored redactions, and erases the edits of deleted messages.
  - Messages are `m.room.message` events that are not edits (`rel_type IS NOT 'm.replace'`): the history, the conversation's last message and the unread count leave edits out; the unread count leaves deleted messages out too.
  - `Relations` returns, per message, its latest edit **by the message's own sender** with an `m.new_content` (Matrix's rule: anyone else's edit is ignored), and its reactions grouped by key, each sender once, in the order they came. Redacted edits and reactions do not count.
  - A redacted event reports `Unsigned.RedactedBecause`.
- **API 1.7** (`core/api/schema/core-api.schema.json`), optional fields of `Message`, so older interfaces ignore them (ADR 0012):
  - `edited`: the text is the latest edit's;
  - `deleted`: the text is empty and nothing else is shown;
  - `reactions`: `{key, count, from_me}` per key;
  - `reply_to`: `{message_id, from_me, sender_name, text, deleted}` of the answered message, with its latest text; only `message_id` when the message is not on the device.
  - A change sends `message.updated` for the message it changes, and `conversation.updated` when that message is the last one or when the unread count may change; it never sends `message.added`.
- **Matrix accounts** (`core/connector/matrix`, ADR 0018): the connector now passes edits, reactions and redactions on as bridgev2 remote events. A reaction's emoji ID is its event ID, the only thing a redaction names. Whether a redaction removes a message or a reaction is looked up in bridgev2's database when the portal asks for the event's type, in its event loop, after the events before it are stored: a reaction taken back right after it came is still found.
- **Echo network** (`core/connector/echo`): `/react [emoji]`, `/edit text`, `/delete text` and `/reply text` make the echo contact react to, edit, delete or answer, to test the whole path without a real network.
- **Interface** (`ui/`):
  - a reply starts with a quote of the answered message ("In reply to" for screen readers; "You", or "An earlier message" when it is not on the device);
  - an edited message says "Edited";
  - a deleted message says "Message deleted", in the thread and in the inbox's preview;
  - reactions are a list of chips (emoji and count), each read out as "🎉: you and 1 other";
  - when a message changes, the replies that quote it change with it.
- **Android notifications** (`MessageNotifications.kt`, ADR 0033): a deleted message's line leaves the notification that shows it (the notification goes when no line is left), and an edited message's line shows its new text. A deleted text must not stay readable on the lock screen.

## Consequences

- Telegram and Signal already send these changes through bridgev2: they now show without connector work. Telegram's and Signal's own message formats (Telegram's custom emoji reactions, Signal's "edit by another device") follow their connectors' conversion.
- The interface shows reactions without saying who reacted beyond "you": `Reaction.Senders` is in the local server, and a later task can expose it.
- A message deleted on the network is erased from the core's local server, but the text may remain where it was copied before the deletion: a notification already dismissed, the system's backup of the app, or a copy of the database. The core does not log message content (`CLAUDE.md` §6.8), so the logs hold none.
- Sending reactions, edits, deletions and replies from Musubee is T2.19: it goes the other way, through bridgev2's Matrix handlers.
- Every page of history costs one more query; measured cost unknown, the relation index keeps it to the page's messages.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| Edits are not messages: the history, the last message and the unread count leave them out | **verified** (Windows, modernc and mattn SQLite) | `core/localmatrix/relations_test.go` `TestRelations` |
| Only the sender's edits count, the latest wins; reactions are grouped by key, each sender once; a redacted reaction is gone | **verified** (Windows) | same test |
| A deletion erases the message's edits and keeps their relation; the deleted message is not unread | **verified** (Windows) | `TestRedactionErasesEdits` |
| Schema 4 upgrades a version 3 database, erasing the edits of messages deleted before | **verified** (Windows) | `TestUpgradeFromV3`, `TestUpgradeFromV2` |
| The core shows an echo reaction, edit, deletion and reply as `message.updated` (no `message.added`), in the history afterwards, with the right unread count | **verified** (Windows) | `core/embedded/changes_test.go` `TestMessageChanges` |
| The desktop app shows reactions, "Edited", "Message deleted" and reply quotes, before and after a restart | **verified** (Windows) | `apps/desktop/e2e/app.spec.ts`, "a network's reactions, edits, deletions and replies change the messages they are about" |
| A Matrix account shows an edit, reactions sent in clear (as mautrix-go does) and encrypted (as Element does), a reaction taken back (also at once) and a deletion from Synapse | **verified on CI** only (Docker Desktop is broken on the maintainer's machine) | `infra/matrixtest` `TestMatrixAccount` |
| Android's notifications drop deleted lines and show edited text, silently, and a message deleted before it was announced is never announced | **verified** on an API 30 emulator (x86), 2026-10-20 | `MessageNotificationsTest`; `NotificationTest` through the real service with the echo's `/delete` and `/edit` (fails without the change) |
| Telegram's and Signal's changes show the same way | **assumed**: their connectors emit the same bridgev2 events; not run against the networks in T2.18 | |
