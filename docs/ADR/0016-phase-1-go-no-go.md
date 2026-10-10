# ADR 0016 — Phase 1 go / no-go: the core runs on the device; what each platform gets, what waits

- **Status**: accepted
- **Date**: 2026-10-10
- **Task**: T1.8
- **Deciders**: Quentin Raimbaud (maintainer), with the design assistant. On 2026-10-10 the maintainer chose to defer the remaining Apple work (T1.7 part 2) to E3.3 and to go on with the product.

## Context

Phase 1 (`docs/TASKS.md`) had one goal: prove that the Go core can run **on the device**, so that the "on-device" connection mode (`CLAUDE.md` §2) is real, not a promise. ADR 0001 chose the stack on the condition that spikes T1.1 to T1.7 confirm it, with the "hosted bridge" mode as the fallback if they did not. This ADR sums up what the spikes established, platform by platform, what was left open, and what phase 2 starts from. It decides nothing new on its own: every figure comes from the ADR cited next to it, where its sources are.

## Verdict per platform

| Platform | Verdict | Established by | Reservations |
|---|---|---|---|
| **Windows** | **Go**, on the device | ADR 0010 (shared library, MSYS2 UCRT64), 0013 (Electron, core as a child process), 0014 (Telegram journey through the interface, on CI) | Code signing and installers are E3.2 work for macOS and Linux, but Windows needs them too before a public release (not planned yet). |
| **Linux** | **Go**, on the device | ADR 0010, 0013 (Playwright journeys on CI under xvfb) | Telegram on the device is tested on Windows and Android, not on Linux desktop: assumed to work (same Go code, same child process), not verified. Packaging is E3.2. |
| **macOS** | **Go**, on the device | ADR 0013 (Playwright journeys on the macOS CI) | Notarization needs the paid Apple account (E3.2). |
| **Android** | **Go with reservations**, on the device | ADR 0011 (JNI in the shared library, foreground service, real Pixel 8 Pro), 0014 (Telegram on an emulator and on the phone, idle battery) | (1) Google Play's acceptance of a `specialUse` foreground service is unknown (fallbacks in ADR 0011: push, or `dataSync` with its 6-hour limit). (2) The connection drops in deep Doze: real-time delivery on an idle phone needs push (E2.6). (3) Round trips of 17 ms on the phone, acceptable but to watch. (4) `mattn/go-sqlite3` instead of the pure-Go driver (seccomp on x86_64). |
| **iOS** | **Hosted mode first; on-device plausible, not proven** | ADR 0015 (core built for iOS, simulator figures) | The real memory limit of the notification extension is **unknown**: measuring it needs a paid Apple Developer account (push notifications are not available to free accounts, verified in Apple's capability table on 2026-10-10), a Mac and the test user's iPhone. Deferred to E3.3, where the paid account is needed anyway (TestFlight). |

**Overall: go.** The stack of ADR 0001 stands: a Go core with `bridgev2` connectors in-process (ADR 0009), a C surface shared by every platform (ADR 0010, 0011), one JSON API contract (ADR 0012), React in Electron and Capacitor. No spike failed; the fallback (hosted bridge) is only needed for iOS at first, as `CLAUDE.md` §3 already planned.

## What phase 1 established

1. **bridgev2 without a homeserver** (ADR 0009): our `core/localmatrix` implements the Matrix side; `bridgev2` connectors run in the app's process with one SQLite database. Verified with our echo connector, then with mautrix-telegram's connector unchanged (ADR 0014).
2. **One C surface for every platform** (ADR 0010, 0011, 0015): `core/ffi`'s header as a shared library on Windows, Linux and Android (with JNI entry points), as an XCFramework of static libraries on iOS. Sizes: the core with Telegram is 54 MB on desktop and 56 MB on Android x86_64 (ADR 0014); it was 17 to 18 MB before Telegram.
3. **One API contract** (ADR 0012): JSON Schema, generated Go and TypeScript types, contract tests on both sides; transports add under 0.2 ms per call. Mobile calls the core in-process; the desktop runs it as a supervised child process over stdio (ADR 0013).
4. **A real network on the device** (ADR 0014): Telegram through the core, on Windows (core and desktop app) and Android (emulator on CI, and the maintainer's Pixel 8 Pro), with test bots against production Telegram (ADR 0006); idle cost on a real phone about 5 mAh per hour.
5. **The iOS extension is a budget question, not a Go question** (ADR 0015): on the simulator, the Go runtime with goolm costs about 4 MiB over an empty process; linking the whole core adds another 7 MiB before any work. The extension will link only what decrypting a notification needs.

## What phase 1 did not settle

Each item names where it is settled. "Unknown" means nobody has measured or read it yet.

| Open point | Status | Settled in |
|---|---|---|
| Real memory limit of the iOS notification extension, figures on a device | unknown | E3.3 (paid account, Mac, iPhone) |
| AGPL and Apple's App Store | unknown, legal (`CLAUDE.md` §3) | before any iOS publication, with a competent human |
| Google Play and the `specialUse` foreground service | unknown | before any Play publication (E2.6 may change the need) |
| Secrets at rest: Telegram's session key is in plaintext in `core.db` | known gap, against `CLAUDE.md` §6.8 | E2.9, before any public release |
| **Native Matrix accounts**: the core as a Matrix client of a real homeserver (sync, end-to-end encryption with goolm, key storage) | **not spiked**. `localmatrix` is the Matrix side of on-device bridges only; a native Matrix account, and the hosted-bridge mode, need a real client | phase 2, first network task (T2.2) |
| **Signal on the device**: mautrix-signal links libsignal, a Rust library, as a static C library (`libsignal_ffi.a`, docs.mau.fi build instructions, read 2026-10-10) | assumed feasible; cross-building Rust for Windows, Android (four ABIs) and later iOS is unmeasured. Signal's staging servers need a real phone number to register (ADR 0005): a manual step of the maintainer | phase 2, Signal spike (T2.6) |
| Conversation merging: data model and sync across devices (`CLAUDE.md` §4, question 5) | not started | T2.1 (data model), E2.5 (feature) |
| UI state management and virtualized message list, measured on low-end Android (`CLAUDE.md` §4, question 6) | input only: re-rendering the open thread costs 1.4 to 7 ms per event (ADR 0013) | T2.4 |
| bridgev2 stops without waiting for its handlers (mautrix/go#602, our report) | worked around (ADR 0010 point 8, revised in T1.7): in-flight work may be lost on stop; connectors must resynchronise | per connector; upstream |
| mautrix-go after v0.31.0 | pending: upgrade at the next release, then remove the workarounds listed in ADR 0009 and 0010 | when it ships |

## The fallback, restated

If a network cannot run on a platform (iOS at first; a network whose library does not build for a target; a store that refuses the app), that account uses the **hosted bridge** mode: a bridge on a server (ours or the user's) with a Matrix homeserver, the app being a Matrix client. The UI says plainly that the bridge's host sees the messages (`CLAUDE.md` §2). This mode needs the same Matrix client as native Matrix accounts, which is why that client comes first in phase 2.

## Phase 2: first tasks

Phase 2 keeps its goal (Android + Windows MVP with native Matrix, Telegram and Signal). Its epics are refined into tasks in `docs/TASKS.md`, ordered so that the risky unknowns come first and so that work that needs the maintainer (Signal's phone number, real-device sessions) does not block the rest:

1. **T2.1** Domain model and the person ↔ conversation link table (E2.1, data side of E2.5).
2. **T2.2** Native Matrix accounts: the core as a Matrix client, end-to-end encrypted, tested against Synapse in Docker (also the base of the hosted-bridge mode).
3. **T2.3** Secrets at rest (E2.9).
4. **T2.4** UI state and virtualized message list (ADR with measurements).
5. **T2.5** Unified inbox in the UI (E2.3), on top of T2.1.
6. **T2.6** Signal on the device (spike, then connector), when the maintainer can register a staging number.

## Consequences

- Phase 2 starts. No change to the stack; ADR 0001's "Revisit if" conditions are not met.
- iOS work stops until E3.3. The core's packages must stay separable so that a minimal extension package can be built without `bridgev2` or the connectors (ADR 0015).
- Before any public release, whatever the platform: secrets at rest (E2.9), a human review of the licensing points (`CLAUDE.md` §3), and the store questions above.
- **Revisit if:** the Matrix client (T2.2) or Signal (T2.6) cannot run in-process on Android or Windows; the device measurement of E3.3 rules out any extension within the limit (then iOS stays in hosted mode, and the extension only decrypts through the hosted bridge's push).

## Sources

- ADR 0001 (stack), 0005 (Telegram test environment, Signal staging), 0006 (Telegram test bots), 0009 (bridgev2 in-process), 0010 (shared library), 0011 (Android), 0012 (API contract), 0013 (desktop shell), 0014 (Telegram on the device), 0015 (iOS extension memory).
- Apple, "Supported capabilities (iOS)", push notifications available to ADP and ADEP members only: https://developer.apple.com/help/account/reference/supported-capabilities-ios (accessed 2026-10-10).
- mautrix, bridge setup (Signal: Rust or a precompiled `libsignal_ffi.a`): https://docs.mau.fi/bridges/go/setup.html?bridge=signal (accessed 2026-10-10).
