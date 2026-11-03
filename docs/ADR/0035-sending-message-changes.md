# ADR 0035 — Sending changes to messages: commands that wait for the network, a refusal undone on the device

- **Status**: accepted
- **Date**: 2026-10-20
- **Task**: T2.19
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

T2.18 (ADR 0034) shows what networks do to messages: edits, deletions, reactions and replies arrive as Matrix events in the core's local server and change the messages they are about. The user could not do any of it: the API only sent text (`messages.send`). E2.4 asks for replies, reactions and editing in the thread.

The way back already exists in bridgev2: a Matrix event that the user sends to a portal room reaches the connector's handler (`HandleMatrixEdit`, `HandleMatrixReaction`, `HandleMatrixReactionRemove`, `HandleMatrixMessageRemove`; replies are `HandleMatrixMessage` with `ReplyTo`), after bridgev2 checks the network's room capabilities (`event.RoomFeatures`: `Reply`, `Edit`, `Reaction`, `Delete`). bridgev2 then reports the outcome as a message status (`com.beeper.message_send_status`), which the local server stores per event (`Server.MessageStatus`, ADR 0009).

Two questions:

1. What does the interface learn, and when? A message's delivery is reported later, by `message.updated` events (status `sending`, then `sent` or `failed`). A change to a message has no status of its own in the API: `Message` describes the message, not the changes made to it.
2. What does the device show when the network refuses a change (a message too old to edit, a contact that blocked the user, an account logged out)? The change is already stored in the local server when bridgev2 hands it to the connector.

Sources: mautrix-go v0.31.0 `bridgev2/portal.go` (`handleMatrixEdit`, `handleMatrixReaction`, `handleMatrixRedaction`, `sendSuccessStatus`, `sendErrorStatus`) and `bridgev2/networkinterface.go` (`EditHandlingNetworkAPI`, `ReactionHandlingNetworkAPI`, `RedactionHandlingNetworkAPI`, `MatrixReactionPreResponse`); `event/capabilities.go` (`RoomFeatures`, `CapabilitySupportLevel.Partial`); `go.mau.fi/util/variationselector`; the Matrix client-server specification, sections "Transaction identifiers", "Event annotations and reactions" and "Redactions".

## Options considered

### How a change is answered

#### Option A — The command waits for the network's answer (chosen)
`messages.react`, `messages.unreact`, `messages.edit` and `messages.delete` store the change, which changes the message at once (`message.updated`, as in T2.18), then wait for the change's message status and answer with it: nothing on success, a `network` error with the network's reason on a refusal. The request's time limit (30 s) bounds the wait.
- Pros: the interface needs nothing new to learn the outcome; a refusal is an error where the user acted; no status per change in `Message`.
- Cons: a command can take as long as the network; a slow network makes the interface's call wait (the change already shows).

#### Option B — Answer at once, report the outcome with an event
- Pros: commands never wait.
- Cons: a new event type, or a status per reaction and per edit in `Message`, for an outcome that is almost always success; the interface must match an event to an action the user may have forgotten.

### What a refusal leaves on the device

#### Option C — Undo a refused reaction or edit; keep a refused deletion (chosen)
A refused reaction or edit is taken back on the device: `Server.Retract` stores a redaction by the user that is not handed to the bridge, so the message is as the network has it. A deletion stays: the user asked for the text to go, and it is gone from the device even when the network keeps it; the command says so (`network` error).
- Pros: the device never shows a reaction or an edit that the other side does not see; a deleted text never comes back.
- Cons: after a refused deletion the device and the network differ (the network keeps a message the device erased); the interface tells the user.

#### Option D — Keep every change, marked as failed
- Cons: a reaction or an edit that the other side never saw stays on the device; marking it needs a status per change (Option B's cost).

### Which actions to offer

#### Option E — Per conversation, from the network's capabilities (chosen)
`Conversation.actions` (`reply`, `react`, `edit`, `delete`) comes from the login's `GetCapabilities` for the portal, each true when its level is at least partial. The interface offers only these; the core checks them again before sending.

## Decision

**Options A, C and E.**

- **API 1.8** (`core/api/schema/core-api.schema.json`):
  - `messages.react` and `messages.unreact` (`conversation_id`, `message_id`, `key`), `messages.edit` (`conversation_id`, `message_id`, `text`), `messages.delete` (`conversation_id`, `message_id`), each answering `{}` once the network did;
  - `messages.send` takes `reply_to`, a message of the same conversation;
  - `Conversation.actions`, omitted when the network allows none;
  - reacting twice with the same key, taking back a reaction the user did not make, or deleting a deleted message changes nothing.
- **Core** (`core/embedded/changes.go`):
  - the target must be a message (`m.room.message`, not an edit) of the conversation; edits and deletions only on the user's own messages, edits only on text and emotes; no reaction, edit or reply on a deleted message;
  - reaction keys take the emoji variation selector (`variationselector.Add`), the form bridgev2 gives the network's reactions, so one emoji counts once whichever side sent it;
  - `change` sends through `Server.SendEvent`, polls the change's status every 50 ms, and on `fail` or `retriable` retracts a reaction or an edit (outside the request's time limit, since the answer came) and answers `network` with the status's message;
  - the statuses of changes are commands' answers: they no longer come out as `message.updated` of the change's event.
- **Local server** (`core/localmatrix`): `SendEvent` sends a message, a reaction or a redaction through the room's bridge (`SendMessage` is its message case); `Retract` redacts one of the user's events without telling the bridge; `Reaction` finds the user's live reaction with a key.
- **Echo network**: allows all four actions, and refuses every change toward its unreachable contact, to test refusals.
- **Matrix accounts** (`core/connector/matrix`): capabilities v2 allow all four. Edits (`m.replace`) and reactions go encrypted when the room is (Element does; mautrix-go never encrypts reactions); redactions go in clear (the homeserver must read them). A sent reaction's emoji ID is its remote event ID, as for received ones (ADR 0034), so bridgev2 recognises it when the sync brings it back, and a later redaction finds it. The sync also brings back the user's own edit: the client skips an edit whose `unsigned.transaction_id` is set, which the homeserver gives only to the client that sent it, so another device's edits still come through.
- **Interface** (`ui/`):
  - each message that allows an action has a "More actions" button in its meta line; it opens, under the bubble, six quick reactions (40 px targets), Reply, Edit and "Delete…";
  - the actions are plain buttons in a fieldset, in the tab order, not an ARIA menu (which needs arrow keys); Escape closes them and gives the focus back to the button; so does a press or a focus elsewhere, listened to on the document because WebKit does not focus a button on click;
  - a deletion asks "Delete this message for everyone?", with the focus on Cancel;
  - a reaction chip is a button that adds or takes back the user's reaction (`aria-pressed`), read out as in T2.18;
  - Reply and Edit go through the composer, which shows what it answers or edits, with a button and Escape to leave; an edit starts from the message's text and saves only a changed text;
  - an action is offered only when the conversation allows it and the message is on the network (`sent` or `received`), not deleted, and the user's own for Edit and Delete;
  - a refused change shows as the app's error banner ("Could not edit the message: …").

## Consequences

- Telegram and Signal offer the actions their connectors declare in `GetCapabilities`, and send them with their own handlers: **assumed** to work, not run against the networks in T2.19.
- A command waits for the network: on a slow network the change shows at once and the call returns later. A change still pending after 30 s answers `timeout`, stays on the device, and may still go through.
- After a refused deletion, the message is erased on the device and kept on the network; the error says so. Option C chose this over a deleted text coming back.
- A reaction taken back that the network refuses stays on the network only: the device shows it gone (there is no way to undo a redaction).
- The menu closes on any press elsewhere, including a scroll's touch: the user opens it again.
- Deleting a message moves the focus to its button, which then goes with the message's actions: keyboard users land on the page. A later accessibility pass can move it to the composer.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The core reacts, takes a reaction back, edits, answers and deletes, each changing the message at once, with the variation selector on keys; refuses empty keys and texts, others' messages for edits and deletions, deleted messages, and messages of another conversation | **verified** (Windows) | `core/embedded/changes_test.go` `TestUserChanges` |
| A refused reaction or edit is undone and fails with `network`; a refused deletion stays and fails with `network` | **verified** (Windows) | `TestRefusedChangesAreUndone` (the echo's unreachable contact) |
| The contract: every new command and field has examples, valid for Go's and TypeScript's validators | **verified** (Windows) | `core/api` `TestExamplesCoverTheSchema`; `ui/src/core-api/contract.test.ts` |
| Which actions a message offers; the user's reaction is found whatever its variation selector; the store's calls and errors | **verified** (Windows) | `ui/src/message.test.ts`, `ui/src/store.test.ts` |
| The desktop app reacts, takes back by pressing the chip, answers, edits and deletes (with the confirmation), by pointer and keyboard (Escape, focus), and keeps it through a restart | **verified** (Windows, Electron 44) | `apps/desktop/e2e/app.spec.ts`, "the user reacts to a message, answers it, edits and deletes their own, from the keyboard or the pointer" |
| A Matrix account sends a reply, an encrypted reaction and its redaction, an encrypted edit and a deletion that bob receives; the own edit's echo is skipped | **verified on CI** only (Docker Desktop is broken on the maintainer's machine) | `infra/matrixtest` `TestMatrixAccount` (`ownChanges`); `TestSentHere` |
| The Android app reacts by touch | **verified on CI** only (the local API 30 emulator has WebView 83, below the app's 111) | `apps/mobile/e2e/android.app.test.mjs` |
| Telegram and Signal send the changes | **assumed** | |
