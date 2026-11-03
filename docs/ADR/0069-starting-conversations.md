# ADR 0069 — Starting a conversation: bridgev2's identifier resolution, from an account the user picks

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.80
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

Until now the user could only write in conversations that the networks brought. Every messenger starts a conversation from a username, a phone number or a contact. Each network decides what it accepts.

Sources (read 2026-11-03), mautrix-go v0.31.0:
- `bridgev2/networkinterface.go`: `IdentifierResolvingNetworkAPI.ResolveIdentifier(ctx, identifier, createChat)`, `ResolveIdentifierResponse`, `CreateChatResponse`;
- `bridgev2/provisionutil/resolveidentifier.go` (`ResolveIdentifier`): resolves, updates the ghost, gets the portal and creates its room when `createChat` is set and it has none; the bridge bot's `start-chat` command (`bridgev2/commands/startchat.go`) goes through it;
- mautrix-telegram v0.2609.0 implements `ResolveIdentifier` for usernames and phone numbers.

## Options considered

#### Option A — The provisioning helper, from an account the user picks (chosen)
`conversations.start` takes an account and what the user typed, and calls `provisionutil.ResolveIdentifier` with `createChat`. It returns the conversation, an existing one when there is one. `Account.starts_conversations` says whether the account's connector resolves identifiers.
- Pros: the same path as bridgev2's own commands; each network keeps its own rules.

#### Option B — A contact list per network first
- Cons: not every network lists contacts (bridgev2's `ContactListingNetworkAPI` is optional); typing is the common ground. A list is a later addition.

### Errors

Any failure answers `not_found` with a fixed text. The network's reason may contain what the user typed (a phone number), and errors are logged.

## Decision

**Option A**, API 1.37.
- The echo network finds its contacts by ID or name, ignoring case.
- The interface:
  - "New conversation…" above the inbox opens a dialog: the account to write from (those that start conversations), "To", and "Start";
  - the conversation then opens.

## Consequences

- Since API 1.38, `accounts.contacts` lists an account's contacts where its connector does (`ContactListingNetworkAPI`, through `provisionutil.GetContactList`); `conversations.start` takes a contact's opaque ID instead of what was typed, and the dialog suggests contacts as the user types. The echo network lists its contacts.
- Groups are not created yet (bridgev2's `GroupCreatingNetworkAPI`).
- Signal's and Matrix's connectors are to be checked for `ResolveIdentifier`; the dialog lists only accounts that have it.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The echo account starts a conversation by name, again opens the same one, and nobody is not found | **implemented, unverified** | `core/embedded/start_test.go` |
| Contacts are listed, and one starts its conversation | **implemented, unverified** | `core/embedded/start_test.go`, `TestContacts` |
| The desktop app starts one from the dialog, from a contact too, and says when nobody is found | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "the user starts a conversation" |
| Telegram finds a username | **assumed** (mautrix-telegram's `ResolveIdentifier`; needs a Telegram account) | |
