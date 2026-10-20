# Architecture Decision Records (ADR)

One ADR per structural decision. Every phase 1 spike ends with a go / no-go ADR. An accepted ADR is not rewritten: it is superseded by a new ADR that cites it.

New ADR: copy [`0000-template.md`](0000-template.md) to `NNNN-short-title.md` (next number, four digits) and add it to the index below.

Every ADR separates what is **verified**, **assumed** and **unknown**, and cites its sources with the date they were accessed.

## Index

| No. | Title | Status | Task |
|---|---|---|---|
| [0001](0001-stack.md) | Tech stack: Go core, React UI, Electron and Capacitor | accepted | T0.1 |
| [0002](0002-license-and-reuse.md) | AGPL-3.0-or-later license, DCO and REUSE check | accepted | T0.1 |
| [0003](0003-third-party-skills-guardrails.md) | Guardrails for third-party Claude Code skills | accepted | T0.2 |
| [0004](0004-integration-test-environment.md) | Integration test environment: Synapse and PostgreSQL with Docker Compose | accepted | T0.3 |
| [0005](0005-telegram-test-environment.md) | Telegram test environment for end-to-end bridge tests | superseded by 0006 (findings valid) | T0.4 |
| [0006](0006-telegram-e2e-with-test-bots.md) | Telegram end-to-end tests with dedicated test bots | accepted | T0.4 |
| [0007](0007-mobile-e2e-tool.md) | Mobile end-to-end test tool: Appium with WebdriverIO | accepted | T0.5 |
| [0008](0008-dependency-license-audit.md) | Automated license audit of Go and npm dependencies | accepted | T0.6 |
| [0009](0009-bridgev2-in-process.md) | Running bridgev2 connectors in-process, without a homeserver | accepted | T1.1 |
| [0010](0010-core-shared-library.md) | The core as a C shared library (Windows and Linux) | accepted | T1.2 |
| [0011](0011-core-on-android.md) | The core on Android: JNI in the shared library, in a foreground service | accepted | T1.3 |
| [0012](0012-core-api-contract.md) | The core API contract: one JSON Schema, generated types, and the transports that carry it | accepted | T1.4 |
| [0013](0013-desktop-shell.md) | The desktop shell: Electron with the core as a supervised child process | accepted | T1.5 |
| [0014](0014-telegram-on-device.md) | Telegram on the device: mautrix-telegram's connector inside the core | accepted | T1.6 |
| [0015](0015-ios-nse-memory.md) | iOS notification extension memory: the core built for iOS, first measurements on the simulator | proposed | T1.7 |
| [0016](0016-phase-1-go-no-go.md) | Phase 1 go / no-go: the core runs on the device; what each platform gets, what waits | accepted | T1.8 |
| [0017](0017-persons-data-model.md) | Persons: merged conversations keyed by network identity, stored next to the bridges | accepted | T2.1 |
| [0018](0018-native-matrix-accounts.md) | Native Matrix accounts: the core as an encrypted Matrix client, behind a bridgev2 connector | accepted | T2.2 |
| [0019](0019-secrets-at-rest.md) | Secrets at rest: sessions sealed in the database, a master key in the OS secure storage | accepted | T2.3 |
| [0020](0020-ui-state-and-thread-list.md) | UI state and the thread list: the app's own store, and a list virtualized with virtua | accepted | T2.4 |
| [0021](0021-unified-inbox.md) | The unified inbox: conversations by activity in pages, merged per person in the interface, and the interface in the Android app | accepted | T2.5 |
| [0022](0022-signal-on-device-spike.md) | Signal on the device, spike: libsignal builds for the core's targets, and mautrix-signal can reach Signal's staging servers with a patch | accepted | T2.6 |
| [0023](0023-read-state.md) | Read state: a read position per conversation in the core, unread counts in the API, read when seen | accepted | T2.7 |
| [0024](0024-merging-in-the-interface.md) | Merging conversations in the interface: from the thread, with likely matches first | accepted | T2.8 |
| [0025](0025-signal-on-device.md) | Signal on the device: mautrix-signal in the core behind a build tag, from a patched copy that can reach Signal's staging service | accepted | T2.6 |
| [0026](0026-signal-secrets-at-rest.md) | Signal's secrets at rest: signalmeow's store seals its secret columns, in our copy | accepted | T2.9 |
| [0027](0027-desktop-key-in-safestorage.md) | The core's master key on macOS and Linux: kept by the desktop app with Electron safeStorage, given to the core on its input | accepted | T2.10 |
| [0028](0028-signal-on-android-ci.md) | Signal on Android in CI: libsignal built per ABI with the app's NDK, the Signal tests run on an emulator | accepted | T2.11 |
| [0029](0029-signal-in-the-android-app.md) | Signal in the Android app: an opt-in Gradle property, libsignal prepared per ABI | accepted | T2.12 |
| [0030](0030-logging-in-again.md) | Logging in to an account again: login.start with an account, which never removes the account | accepted | T2.14 |
| [0031](0031-starting-over-after-a-lost-key.md) | Starting over after a lost key: the shell sets the old data aside, never deletes it, and starts a new core | accepted | T2.15 |
| [0032](0032-onboarding-in-plain-words.md) | Onboarding in plain words: the core words its networks' logins, the interface welcomes the user | accepted | T2.16 |
| [0033](0033-message-notifications-on-android.md) | Message notifications on Android: the service decides from the core's events, without push | accepted | T2.17 |
