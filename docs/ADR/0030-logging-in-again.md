# ADR 0030 — Logging in to an account again: login.start with an account, which never removes the account

- **Status**: accepted
- **Date**: 2026-10-12
- **Task**: T2.14
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

A network can end the session of an account: Telegram when the user ends it from another device, Signal when the phone unlinks the device, a Matrix homeserver when the access token is revoked. The connector then reports the bridge state `BAD_CREDENTIALS`, which the core's API shows as the account state `bad_credentials` (ADR 0012), and the interface as "Needs you to log in again".

Until T2.14 nothing could be done with such an account but log out, which removes its conversations from the device (`accounts.logout`), then add it again, which starts from an empty history. E2.2 asks for account management without Matrix jargon; logging in again is the first thing a user needs from it.

bridgev2 (mautrix-go v0.31.0) already models this:

- `bridgev2.LoginProcessWithOverride` (`bridgev2/login.go`): `StartWithOverride(ctx, override *UserLogin)` starts a flow "with the intent of re-authenticating an existing login". `LoginProcessWithParams.StartWithParams` replaces it with `LoginStartParams{Override}`.
- `User.NewLogin` (`bridgev2/userlogin.go`): a login completed with the ID of an existing login of the same user reuses that login (its portals, so its rooms and history), updates its name, profile and metadata, and loads a new client.
- The provisioning API (`bridgev2/matrix/provisioninglogin.go`, `handleCompleteStep`) and the bot commands (`bridgev2/commands/login.go`, `doLoginStep`) delete the overridden login, with `LogoutRemote: true`, when the login completes on another remote ID ("Login resulted in different remote ID than what was being overridden").

Connectors (checked in their code): mautrix-telegram v0.2609.0 implements `StartWithOverride` for its phone, QR code and bot flows (`pkg/connector/loginphone.go`, `loginqr.go`, `loginbot.go`): the phone flow reuses the account's phone number and goes straight to the code, and a bot account refuses the phone and QR flows. Our copy of mautrix-signal and our Matrix connector do not: they start as for a new account.

## Options considered

### Option A — login.start takes an account_id (chosen)
`login.start` gets an optional `account_id` (API 1.5). The core finds the account on the requested network and starts the process with `StartWithParams` or `StartWithOverride` when the connector has them, `Start` otherwise.
- Pros: one more optional parameter, no new command; the interface reuses its whole login screen; every connector benefits from what bridgev2 already does (`NewLogin` reuses the login whenever the same remote account logs in).
- Cons: the interface must know which flows fit an account (it offers the flows of the account's network; a connector refuses a flow that does not fit, as Telegram does for bots).

### Option B — A separate accounts.relogin command
- Pros: the intent is explicit in the command name.
- Cons: a second entry point into the same login processes, with the same steps; nothing it carries that `login.start` cannot.

### Option C — No relogin: log out, then add the account again
- Pros: no API change.
- Cons: the user loses the history kept on the device, and every link of the account's conversations to persons stays dormant until the same conversations come back (ADR 0017); the reason for the task.

## Decision

**Option A.** And one difference from bridgev2's provisioning API: **a login that completes on another remote account does not remove the account being logged in to again.** The new account is added beside it, and the old one stays, still `bad_credentials`, with its conversations, until the user logs out of it.

bridgev2 deletes the old login there, with its portals: for a Matrix bridge that only means leaving rooms that the homeserver keeps. For the core, the portals' rooms are the device's copy of the history (ADR 0009), and deleting them would remove conversations the user did not ask to remove, after a login that may be a mistake (another phone number, another QR code scanned). `accounts.logout` asks for a confirmation in the interface; this path would not.

## Consequences

- **API 1.5**: `LoginStartParams.account_id`, a minor version: the schema forbids unknown fields, but the core decodes parameters with `encoding/json`, which ignores them, so a core of API 1.4 would start an ordinary login instead (which still reuses the account when the same remote account logs in). The interface and the core ship together.
- **Errors**: an `account_id` that is not an account of the requested network is `not_found`.
- **Interface**: an account in state `bad_credentials` shows "Log in again", which opens the login screen titled "Log in again to <account>" with the flows of the account's network only, and the hint that its conversations stay on the device.
- **Echo network**: usernames starting with `expired` connect right after the login and report `bad_credentials` from the next start of the core on, as `offline` ones report `transient_disconnect`; its username flow implements `StartWithOverride` and fills in the username. Logging in again therefore works only until the next restart, by design.
- **Telegram's phone flow** logs the reused phone number at debug level (`loginphone.go`, "Using existing phone number for relogin"). The apps run the core at `info` (`CoreService.kt`, and the core's default), so it is not written there; a debug log of a relogin contains the number.
- **Different remote account**: the old account remains until the user logs out of it. A later task may offer, in that case, to move the person links of the old account's conversations; nothing does it now.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| An account whose session ended keeps its ID, its conversations and its history when the user logs in again, and works again | **verified** (Windows; core tests with the race detector) | `core/embedded/relogin_test.go` `TestLoginAgainKeepsTheConversations` |
| The flow of logging in again fills in the account's username (echo) | **verified** | same test |
| A login again that ends on another remote account adds it and keeps the first | **verified** | `TestLoginAgainOnAnotherAccountKeepsBoth` |
| An account_id unknown, of another network, or logged out is `not_found` | **verified** | `TestLoginAgainNeedsAnAccountOfTheNetwork` |
| The desktop app shows the ended session, logs in again from the account's button and keeps the conversations | **verified** (Windows, Electron) | `apps/desktop/e2e/app.spec.ts`, "an account whose session ended is logged in to again" |
| Telegram's relogin through `StartWithOverride` goes straight to the code with the account's phone number | **assumed** from mautrix-telegram's code; not run (the CI's Telegram accounts are bots, whose sessions do not end) | `pkg/connector/loginphone.go` |
| Signal: unlinking the device from the phone, then linking again from "Log in again", reuses the account | **unknown**: Signal's connector has no override; whether the new link gets the same login ID (the account's ACI) is expected but not run | |
| The Android app's "Log in again" | **assumed**: the same interface; not run on a device | |
