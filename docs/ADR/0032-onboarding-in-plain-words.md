# ADR 0032 — Onboarding in plain words: the core words its networks' logins, the interface welcomes the user

- **Status**: accepted
- **Date**: 2026-10-15
- **Task**: T2.16
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

Until T2.16, a user without accounts saw "Add an account" and a row of buttons per network: "Telegram: Phone Number", "Telegram: QR Code", "Telegram: Bot token", "Signal: QR", "Matrix: Password". Nothing said what Musubee is, what each network brings, or where the messages go. The words came straight from the network connectors (`GetLoginFlows` and the login steps of bridgev2), which mautrix wrote for Matrix users who log in by talking to a bridge bot:

| Where | Connector's words | Problem |
|---|---|---|
| Telegram flows | "Phone Number", "QR Code", "Bot token" ("Log in as a bot using the bot token provided by BotFather") | A developer's flow shown next to the user's, with the same weight |
| Telegram QR code step | "Scan the QR code on your phone to log in" | Does not say where Telegram's scanner is |
| Telegram password step | "You have two-factor authentication enabled." | Jargon; does not say what to type |
| Telegram wrong code | "Incorrect code" | Does not say what to do |
| Signal flow | "QR" ("Scan a QR code to pair the bridge to your Signal app") | "Bridge" |
| Signal QR code step | "Scan the QR code on your Signal app to log in" | Does not say where Signal's scanner is |

E2.2 asks for onboarding without Matrix jargon. The interface knows no network by design (`AddAccount.tsx` walks whatever steps the core describes), so the wording has to come from the core.

## Options considered

### Option A — The core words its networks, by stable IDs (chosen)
`core/embedded/wording.go` holds a table per network: a description of the network, and the name, description and `advanced` flag of each flow, the instructions of each step and the name and description of each field, keyed by the flow, step and field IDs of the connectors. `networks.list` and the login steps apply it before they leave the core; an ID absent from the table keeps the connector's words.
- Pros: the interface stays free of network knowledge; one place for every shell; the IDs are constants of the connectors (mautrix-telegram exports them), so a rename breaks the build or a test rather than the screen.
- Cons: the table can lag behind a connector that adds a step; the user then sees that step in the connector's words, which is what happened before.

### Option B — Patch the connectors
- Pros: the words live with the code that shows them.
- Cons: mautrix-telegram is a module we do not copy; patching it means a fork for wording. The Signal copy (ADR 0022) already carries patches we want to keep few.

### Option C — Word the networks in the interface
- Pros: the interface's words would be next to its layout, and translatable with it.
- Cons: the interface would learn network IDs and step IDs, breaking the rule that it knows no network; every future user interface (the web app) would repeat the table.

## Decision

**Option A.**

- **API 1.6** (`core/api/schema/core-api.schema.json`): `Network.description` (optional) says what an account of the network brings; `LoginFlow.advanced` (optional, omitted when false) marks a flow that most people do not need. Both are optional additions, so older interfaces ignore them (ADR 0012).
- **Core** (`core/embedded/wording.go`):
  - Network descriptions for every network, echo included ("Pretend contacts that answer you, to try Musubee without an account.").
  - Telegram: "With your phone number", "By scanning a QR code", and "As a bot", which is advanced. Its QR code step says where the scanner is; its password, wrong code and wrong password steps say what to type and where to reset the password.
  - Signal: "By linking this device", and a QR code step that says where the scanner is.
  - Matrix is our connector: its flow is renamed in `core/connector/matrix` ("With your password").
  - Echo's flows keep their names ("Username", "Code", "QR code"): it is a test network, and its words are not jargon.
- **Interface** (`ui/src/app/AddAccount.tsx`):
  - Without accounts, the screen opens with "Welcome to Musubee": what it does, and that it connects from the device straight to each network, so messages do not go through a Musubee server.
  - Each network shows its description, then one button per flow with the flow's name and, below it, its description (visible, rather than in a `title` tooltip that touch screens never show).
  - A button's accessible name stays "Network: Flow" (`aria-label`, which Android's WebView exposes, ADR 0029), its description comes from `aria-describedby`.
  - Advanced flows sit under a collapsed "Other ways to log in".

### Where the menus are

The QR code steps name the menus of the phone apps. They differ between platforms and versions, so the instructions name both labels where they differ:

- Telegram: Settings, then Devices, then "Link Desktop Device" (iOS, and some Android versions) or "Scan QR Code" (Android), per secondary guides that disagree on which versions ([itgeared](https://www.itgeared.com/how-to-login-to-telegram-using-qr-code/), [boei.help](https://boei.help/blog/telegram-web-qr-code/)); Telegram's own documentation does not describe the menu.
- Signal: Settings, then Linked devices, then a "+" button (Android) or "Link New Device" (iOS), per [Signal's support article](https://support.signal.org/hc/articles/360007320551) as summarised by a search engine (the page refuses automated reads).

## Consequences

- "Your messages do not go through a Musubee server" is true while every network runs on the device (`CLAUDE.md` §2, on-device mode). The hosted-bridge mode (E3.1, and iOS first, E3.3) must change it, per account: the welcome text is the place where a wrong promise would be most visible.
- The words are English only, like the rest of the interface. Translation (later) will need the core's words to be translatable too, or keyed for the interface to translate.
- Network order is still the core's: echo first. A product build should put the networks people use first, or hide echo (follow-up).
- A connector that adds or renames a step shows its own words until the table follows. `TestWordingFollowsTheConnectors` catches a renamed Telegram flow; the Signal-tagged test checks the Signal step ID against mautrix-signal's constant.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| `networks.list` gives every network a description, and Telegram's flows their new names, with only the bot flow advanced | **verified** (Windows) | `core/embedded/wording_test.go` `TestTelegramLoginInPlainWords`, `embedded_test.go` `TestNetworksList` |
| Telegram's first phone step comes with the new field description, without contacting Telegram | **verified** (Windows) | same test |
| A step absent from the table keeps the connector's words, and the connector's shared steps are not changed in place | **verified** (Windows) | `TestWordingOfAStep` |
| The table names only flows that the Telegram connector offers; Signal's step ID is mautrix-signal's | **verified** (Windows, with libsignal) | `TestWordingFollowsTheConnectors`, `signal_test.go` `TestSignalIsOffered` |
| The desktop app welcomes a user without accounts, shows the descriptions, keeps the bot flow under "Other ways to log in", and does not welcome again once an account exists | **verified** (Windows) | `apps/desktop/e2e/app.spec.ts`, "a first launch says what Musubee is…" |
| Android's accessibility tree exposes the flow buttons as "Network: Flow" | **verified** on a Pixel 8 Pro (Android 17), 2026-10-20, and by CI's Appium journey ("Echo: Username") | UI dump of the app: buttons "Echo: Username", "Telegram: With your phone number", "Telegram: As a bot" |
| The Android app welcomes a user without accounts, shows the descriptions, opens "Other ways to log in", logs in on echo, and no longer welcomes once an account exists | **verified** by hand on a Pixel 8 Pro (Android 17), 2026-10-20 | |
| The menu names of Telegram's and Signal's phone apps | **assumed** from the sources above; not checked on a phone | |
| Telegram's password reset is in Settings, Privacy and Security, Two-Step Verification | **assumed** (the app's menu names as known in 2026); not checked on a phone | |
