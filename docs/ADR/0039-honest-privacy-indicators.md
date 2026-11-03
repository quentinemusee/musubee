# ADR 0039 — Honest privacy indicators: the core says who can read each conversation, unknown is never safe

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.23
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

`CLAUDE.md` §2 sets two connection modes per account (on the device, or through a hosted bridge that reads the plaintext) and asks that "the UI must say so honestly". E2.8 asks for honest indicators of the connection mode.

The connection mode is not the whole answer. Who can read a conversation depends first on the network:

- Signal is end-to-end encrypted, always; the core is a linked device of the user's account (ADR 0025).
- Telegram's ordinary chats ("cloud chats") are encrypted between the app and Telegram's servers only: Telegram can read them. Only "secret chats" are end-to-end encrypted, and mautrix-telegram v0.2609.0 has none: its `ROADMAP.md` lists "Secret chats (i.e. end-to-bridge encryption on Telegram)" as not done, and its connector has no code for them (`pkg/connector`, searched for `EncryptedChat` and secret chats).
- A Matrix room is end-to-end encrypted when its state has `m.room.encryption`; otherwise every member's homeserver reads it (ADR 0018).
- The echo network is a test network inside the core: nothing leaves the device.

An interface that showed a padlock for "on the device" would tell a Telegram user that nobody else reads their chats, which is false.

Sources: mautrix-telegram v0.2609.0 `ROADMAP.md` and `pkg/connector/`; mautrix-go v0.31.0 `StateStore.IsEncrypted`; Telegram's FAQ on cloud chats and secret chats; the Matrix client-server specification, "End-to-end encryption" (`m.room.encryption`).

## Options considered

### Option A — The core says who can read, per conversation; the interface words it (chosen)
`Conversation.privacy` has a `level` and, when someone besides the members reads, `readers` in plain words; `Account.connection` says where the account's bridge runs. The levels are what can be told apart: `end_to_end`, `network_can_read`, `bridge_can_read`, `device_only`, `unknown`.
- Pros: the interface knows no network (as for the logins' wording, ADR 0032), and a network added later can only be `unknown` until the core knows better; a Matrix room's level follows its state.
- Cons: the levels are coarse: they do not say whether metadata (who talks to whom, when) is visible, which it is on every network.

### Option B — One indicator per account (on the device / hosted bridge)
- Cons: says nothing true about Telegram or about Matrix rooms without encryption; reassures where it should not.

### Option C — The interface maps network IDs to levels
- Cons: the interface would have to know networks, and Matrix rooms differ within one network.

## Decision

**Option A.**

- **API 1.12**: `Account.connection` (`on_device`, `hosted_bridge`), `Conversation.privacy` (`ConversationPrivacy`: `level`, `readers`), `PrivacyLevel`. Both fields are omitted by older cores; interfaces take a missing level as unknown.
- **Core** (`core/embedded/privacy.go`): echo `device_only`; Signal `end_to_end`; Telegram `network_can_read` by "Telegram"; Matrix `end_to_end` when the room's state is encrypted (the connector's `RoomEncrypted`), else `network_can_read` by "the Matrix servers of the room's members"; anything the core cannot tell (another network, a Matrix room whose state cannot be read, no login) `unknown`. Every account is `on_device` until hosted bridges exist.
- **Interface** (`ui/src/privacy.ts`, `ui/src/app/PrivacyNote.tsx`): under the thread's header, a label that opens its explanation:
  - "End-to-end encrypted": "Only the people in this conversation can read its messages, on their own devices", and where Musubee connects from;
  - "Not end-to-end encrypted": "Telegram can read this conversation's messages, as in Telegram's own app";
  - "Encrypted up to the bridge": who runs the bridge, and that it passes the messages on;
  - "Test conversation": the messages never leave the device;
  - "Encryption unknown": "take them as readable by" the network.
  Only end-to-end encryption on the device and the test network reassure (🔒); every other label is marked ⚠️ and in the error color, and says so in words.

## Consequences

- Telegram users are told, in every conversation, that Telegram reads it. This is true of Telegram's own app too; the explanation says so, so that it does not read as a fault of Musubee.
- Metadata is not covered: every network sees who talks to whom and when. A later wording can add it, once the interface has a place for a longer explanation.
- A Matrix room that turns encryption on shows it the next time its conversation is sent (`conversation.updated` comes with messages).
- When hosted bridges exist (E3), `bridge_can_read` and `hosted_bridge` are ready; the core will fill `readers` with the bridge's host.
- Key backup and recovery, the other half of E2.8, are left to a later task.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| Each network's level; Matrix by the room's state; unknown when the core cannot tell | **implemented, unverified** | `core/embedded/privacy_test.go` `TestPrivacy` |
| The echo network's conversations are `device_only`, its account `on_device`, through the API | **implemented, unverified** | `TestPrivacyInTheAPI` |
| An encrypted Matrix room is `end_to_end` | **implemented, unverified** (CI only: Synapse) | `infra/matrixtest` `TestMatrixAccount` |
| The contract: the new fields have examples, valid for Go's and TypeScript's validators | **implemented, unverified** | `core/api` `TestExamplesCoverTheSchema`; `ui/src/core-api/contract.test.ts` |
| The words for each level, never safe when unknown or through a hosted bridge | **implemented, unverified** | `ui/src/privacy.test.ts` |
| The desktop thread shows the label and opens its explanation by pointer and keyboard | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "the thread says who can read the conversation" |
| Telegram and Signal conversations show their levels in the apps | **assumed** (the levels come from the network, not from a run) | |
