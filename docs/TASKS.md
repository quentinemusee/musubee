# Task breakdown — Musubee

Format: **ID — title**. Each task has a deliverable, acceptance criteria and tests. Claude Code handles them in order, one branch per task. Phases 2+ are deliberately less detailed: they will be refined after the phase 1 go/no-go.

Legend: 🧪 = mandatory test · 🍎 = requires a Mac · 💶 = requires a paid Apple account · 📱 = requires a real device

---

## Phase 0 — Foundations and test environment

**T0.1 — Repository skeleton and license**
- Deliverable: the layout of `CLAUDE.md` §5, `LICENSE` (AGPL-3.0), `NOTICE`, `CONTRIBUTING.md` with DCO, `docs/ADR/0001-stack.md`, SPDX headers.
- Acceptance: the repository clones and every directory has a README; the license-check command runs.
- 🧪 Test: a CI script that fails if a source file has no SPDX header.

**T0.2 — Install and verify the skills**
- Deliverable: skills installed according to `docs/SKILLS.md`, list of those actually loaded (`/plugin` then `/reload-plugins`, restart the session if needed).
- Deliverable: run `scripts/install-skills.ps1` and `scripts/install-plugins.ps1` (`caveman` plugin) — or their `.bat` / `.sh` equivalents — fix any skill name that has changed, commit the fixed script.
- Acceptance: `npx skills list` shows ~38 skills; for each area (Go, React, Capacitor, Swift, Android, Electron, tests), a control question makes Claude cite the right skill.

**T0.3 — Integration test environment**
- Deliverable: `infra/compose.test.yml` with Synapse, PostgreSQL, dummybridge; start and stop script; documentation.
- Acceptance: a Matrix test client creates a room, sends a message, reads it back.
- 🧪 Test: `go test -tags=integration ./infra/...` starts the environment, checks service health, shuts it down (integration tests carry the `integration` build tag, see ADR 0004).

**T0.4 — Telegram test environment**
- Deliverable: Telegram bridge connected to the local Synapse and to Telegram. Telegram's test servers turned out unusable (ADR 0005), so the test uses two dedicated test bots in a private channel (ADR 0006).
- Acceptance: a message flows between two Telegram test parties (bots) through the bridge, both ways.
- 🧪 Test: automated E2E, usable in CI. Also record in the ADR what exists (or not) for Signal.

**T0.5 — Multi-platform CI**
- Deliverable: pipeline with Linux, Windows and macOS runners; Android job on an emulator; iOS job on a simulator 🍎. Choice of the mobile E2E tool (Maestro or Appium) documented in an ADR.
- Acceptance: a "hello test" passes on every target.

**T0.6 — Automated license audit**
- Deliverable: audit tool for Go and npm dependencies, wired into CI, with a list of justified exceptions.
- 🧪 Test: deliberately add an incompatible dependency on a test branch → CI fails.

---

## Phase 1 — Feasibility spikes (go / no-go)

Goal: prove that the Go core can run **on the device**. Each spike ends with an ADR with a clear conclusion (feasible / feasible with reservations / not feasible) and measured figures.

**T1.1 — bridgev2 connector without a homeserver**
- Question: can a `bridgev2` connector run in-process, with a local implementation of the "Matrix" interface?
- Deliverable: our own minimal **echo connector** written with `bridgev2` (AGPL; replaces Beeper's dummybridge, which has no license, see ADR 0004): one login flow, fake contacts and conversations, messages echoed back, a few failure modes (delayed echo, failed send). Proof of concept running it in-process (send and receive in a Go test), ADR.
- Rule: do not read or copy dummybridge's code (no license). Learning from AGPL-compatible mautrix bridges is allowed once their license is checked.
- 🧪 Test: the message goes through the connector and reaches local storage, without Synapse.
- Outcome: feasible (go), see ADR 0009. Requires Go 1.27.1 or later.

**T1.2 — Core as a shared library (Windows and Linux)**
- Deliverable: core built as a shared library (cgo), called from a small host program; size and memory measurements. Document the C toolchain required on Windows.
- 🧪 Test: round-trip call through FFI, without memory leaks (long-running loop test).
- Outcome: go on Windows and Linux, see ADR 0010. C interface in `core/ffi/musubee.h`; Windows toolchain MSYS2 UCRT64; SQLite stays pure Go (`modernc.org/sqlite`).

**T1.3 — Core on Android**
- Deliverable: AAR or library via the NDK (evaluate `gomobile bind` and the shared library, compare in an ADR), minimal Capacitor plugin, foreground service.
- 📱 🧪 Test: send and receive on an emulator, then a real device; battery and memory measurements over 1 h.
- Outcome: go with JNI entry points in the shared library (gomobile dropped), core in a `specialUse` foreground service; see ADR 0011. Verified on an API 30 emulator and on a Pixel 8 Pro (Android 17); the x86_64 emulators of the CI (API 30 and 35) found that the pure-Go SQLite is killed by seccomp on Android x86_64, so Android builds use `mattn/go-sqlite3` (ADR 0011 point 7). Open points: Google Play's acceptance of `specialUse`, 17 ms round trips on the phone, and the idle battery cost (the one-hour soak ran during a call in another app).

**T1.4 — Core ↔ UI API contract**
- Deliverable: versioned schema (commands and event streams), TypeScript type generator, ADR comparing direct FFI and a local socket.
- 🧪 Test: contract tests on the Go side and the TypeScript side against the same schema.
- Outcome: JSON Schema 2020-12 in `core/api/schema`, version 1.0, with Go and TypeScript types generated by an in-house generator; contract tests on both sides with two independent validators, and every response and event of the core validated in its tests; see ADR 0012. Transports measured from Node: every one adds under 0.2 ms per call, so mobile stays in-process and the desktop will run the core as a child process over stdio (T1.5). Also fixed: the SQLite pool reopened connections on every message (round trip 11 ms → 4 ms on Windows).

**T1.5 — Electron shell**
- Deliverable: Electron window with a minimal UI, core as a sidecar or library, `contextIsolation` enabled, validated IPC.
- 🧪 Test: Playwright, "open the app, send a dummy message" journey.
- Outcome: the core runs as its own program (`core/cmd/musubee-core`) over stdio, supervised by Electron's main process (restart with backoff, gives up after five crashes in a minute); the interface is served on `app://musubee` with a strict content security policy, in a sandboxed and context-isolated window, and every request is checked against the core API schema before it reaches the core; see ADR 0013. Playwright journeys pass on Linux, Windows and macOS in CI: send and echo, restart with the history kept, isolation checks, recovery after the core is killed. Measured on the three systems: the main process reaches the core as fast as plain Node did in ADR 0012, the IPC adds 0.1 to 0.3 ms per call, the core starts in 60 to 420 ms, and re-rendering the open thread on every event adds 1.4 to 7 ms to a round trip, an input for the virtualized list ADR. Also: a minimal React interface, golangci-lint and oxlint in CI.

**T1.6 — Telegram on the device**
- Deliverable: Telegram connector in "on-device" mode inside the core, against the test servers.
- 🧪 Test: automated E2E on Windows and emulated Android.
- 📱 Battery: one hour on a real phone left idle (screen off, no call or other app in use), connected to Telegram, to measure the idle cost of the service that T1.3 could not isolate (ADR 0011).
- Outcome: mautrix-telegram's connector runs inside the core (`core/connector/telegram`), with a pure-Go stand-in for its cgo WebP encoder, the application credentials from the environment (desktop) or the build (Android), and a new API command `accounts.logout` (API 1.1); see ADR 0014. Automated journeys against production Telegram with the test bots pass on CI: the core alone, the desktop app through its interface (Windows) and the Android app on an API 35 emulator (bot login 6 to 11 s, a post reaches the core in 3 to 5 s, an answer is seen by the other bot in under 0.3 s, Go heap about 2 MiB). The core grows from 18 to 54 MB (desktop) and from 17 to 56 MB (Android x86_64). Battery, one hour idle on the Pixel 8 Pro with the maintainer's account (logged in by phone number): about 5 mAh for the app (8.6 % of what the phone attributed, mostly the radio), 2.45 s of CPU; but the connection dropped and came back 14 times during deep Doze, as the service holds no wake lock, so real-time delivery on an idle phone needs push (E2.6). Known gap: the Telegram session key is stored in plaintext in `core.db`, against `CLAUDE.md` §6.8; to fix before any public release (encrypted database with a key from the OS keystore).

**T1.7 — iOS NSE memory budget** 🍎 💶 📱
- Deliverable: core (or lighter crypto) built for iOS arm64 (evaluate `gomobile bind` to an xcframework), integrated into a test NSE; measurement of real memory on an iPhone (tools: `debugging-instruments`, `ios-memgraph-analysis`).
- Acceptance: a "feature → peak memory" table; written decision: full Go / lighter crypto / hosted mode only for iOS.
- 🧪 Test: the measured threshold becomes a regression test.
- Part 1 done (2026-10-10, ADR 0015, proposed): the core and goolm build for iOS as an XCFramework (`apps/mobile/ios-probe/build-go-xcframework.sh`); a test app with a notification extension; the `ios-memory` CI job measures the probe on the simulator for both SQLite drivers. `simctl push` does not run extensions, so the simulator figures come from bare processes. Outcome so far: on the simulator, the Go runtime with goolm costs about 4 MiB over an empty process, linking the whole core another 7 MiB, the work about 4 MiB; the extension will link only what it needs, and iOS will use `mattn/go-sqlite3`. The CI fails if the simulator's peak goes over a budget (30 MiB with mattn).
- Part 2 (needs the maintainer: a Mac or signing secrets for the macOS CI, a paid Apple Developer account, the test user's iPhone): run the test app on the device (`apps/mobile/ios-probe/README.md`), read the extension's real limit, decide full Go / lighter crypto / hosted mode only, and add the device's threshold to the regression test.

**T1.8 — Go / no-go decision**
- Deliverable: a summary ADR in `docs/ADR/`: what is feasible on each platform, what is postponed, fallback plan (hosted-bridge mode).
- Outcome: **go** (ADR 0016). Windows, Linux and macOS: on the device. Android: on the device, with reservations (Play and `specialUse`, push needed in deep Doze). iOS: hosted mode first; on-device plausible but its extension's memory limit is unmeasured, deferred to E3.3 with the paid Apple account. Not spiked and moved to phase 2: native Matrix accounts (the core as a Matrix client, also the base of the hosted-bridge mode) and Signal on the device (libsignal is Rust).

---

## Phase 2 — Android + Windows MVP (networks: native Matrix, Telegram, Signal)

Order and reasons: ADR 0016. Tasks first, then the epics they belong to. 📞 = needs a manual action of the maintainer (a phone number, a real device).

**T2.1 — Domain model and person links**
- Deliverable: the domain layer of the core (`Account`, `Conversation`, `Message`, `Person`) and a local link table person ↔ conversations in the core's database; API commands to list persons and to link or unlink a conversation (minor API version); an ADR on the data model and on how the links will sync across devices, encrypted (`CLAUDE.md` §4, question 5).
- Acceptance: no Matrix identifier or type reaches the API (checked by a test over the echo and Telegram journeys); the links survive a restart.
- 🧪 Test: unit tests of the domain layer, contract tests of the new commands on both sides.
- Outcome: persons in the core (`core/persons`, ADR 0017), API 1.2 (`persons.*` commands, `person.*` events, `Conversation.person_id`). A link names a conversation by its identity on its network, not by its local room: it survives restarts, stays dormant while its account is absent, and wakes up when the account comes back; these identities are the same on every device, which the encrypted sync will need (rules recorded in the ADR, not built). Only direct conversations can be linked. Every response and event of the core's tests, and of the Telegram journey, is checked for Matrix identifiers (`apitest.MatrixIDs`). The interface's store keeps the persons; showing them is T2.5. Not run: the refusal of group conversations (the echo network has none; T2.5 adds one).

**T2.2 — Native Matrix accounts**
- Deliverable: a `matrix` network in the core: sign-in to a homeserver, sync, rooms as conversations, text sent and received, end-to-end encryption with goolm and a crypto store in the core's database; ADR (it is also the client of the hosted-bridge mode).
- Acceptance: an encrypted conversation between two accounts works through the core API, and survives a restart of the core.
- 🧪 Test: integration tests against Synapse in Docker (`infra/testenv`), never a mock.
- Outcome: a `matrix` network in the core (`core/connector/matrix`, ADR 0018): a bridgev2 connector in which the core is an end-to-end encrypted Matrix client of the user's homeserver (mautrix-go's `cryptohelper` on goolm), with its crypto and state stores in the core's database; logout deletes the device on the homeserver and its keys in the database. Every Go build of the core now needs the `goolm` tag. The journey against Synapse (`infra/matrixtest`: an encrypted direct conversation both ways, a restart, logout, nothing sensitive in the log) runs on CI only: Docker Desktop does not start on the maintainer's machine. Left for later tasks: group invitations (accepted in another client for now), edits, media content, key backup and verification (E2.4, E2.8); the pickle key and the access token are not protected until T2.3.

**T2.3 — Secrets at rest** (E2.9)
- Deliverable: ADR comparing an encrypted database and encrypting the secrets only, with the key in the OS secure storage; implementation on Windows (DPAPI or Credential Manager) and Android (Keystore).
- Acceptance: no session key (Telegram, Matrix) is readable in the database file.
- 🧪 Test: on Windows CI and the Android emulator, with the real key stores.
- Outcome: the accounts' sessions (`user_login.metadata`: Telegram's `auth_key`, Matrix's access token) are sealed with AES-256-GCM by a wrapping `database/sql` driver (`core/storage/sqlite`, `OpenSealed`), under keys derived from a 32-byte master key (`core/secrets`, ADR 0019); the Matrix pickle key is derived from it too, instead of a constant. The master key is kept with DPAPI on Windows (`core.key`) and wrapped by the Android Keystore on Android (`CoreKey`, passed as `database_key`); a wrong key stops the core at start, and plain sessions of older databases are sealed then purged from the file (`VACUUM`). Verified on Windows (DPAPI) and the Android emulator (API 30); the Matrix and Telegram journeys check the database files on CI. Whole-database encryption (SQLCipher) is deferred: it needs cgo everywhere. Matrix logins made before T2.3 must sign in again. Follow-ups: the key of macOS and Linux through Electron `safeStorage` (a plain file until then), the iOS Keychain (E3.3), recovering from a lost key in the interface (E2.2).

**T2.4 — UI state and virtualized message list**
- Deliverable: ADR choosing state management and a virtualized list (`CLAUDE.md` §4, question 6), with measurements on a throttled Android emulator, then on a low-end phone 📞 when available.
- 🧪 Test: a performance test of a long conversation (thousands of messages), kept as a regression test.
- Outcome: the app's own store stays (no state library), with a history cursor per conversation, `loadOlder()` (pages of 100 older messages) and a fast path in `mergeMessages`; the thread is virtualized with virtua (MIT, 4 KB gzip) behind `MessageList`, which opens at the newest message, follows new ones, reads older ones as the user scrolls up and keeps the message being read in place (ADR 0020). Chosen against plain DOM, `content-visibility`, react-virtuoso and @tanstack/react-virtual with a benchmark (`ui/bench/`) on Electron and on the Android 11 emulator, CPU throttled up to 4 times, up to 20 000 messages. The regression test (`npm run bench:check`) measures `MessageList` in Electron on every pull request. Verified on Windows (Electron) and the emulator (WebView 83, with a build lowered for it); the low-end phone 📞 is still to come. Found on the way: the interface needs WebView 111 or later (to enforce with Capacitor's `minWebViewVersion` when the Android app loads it); browser find-in-page only sees the rendered messages, so conversation search goes to the core (E2.4).

**T2.5 — Unified inbox** (E2.3)
- Deliverable: the conversation list of every account, merged per person (T2.1), with pagination and offline display.
- 🧪 Test: Playwright on the desktop, Appium on Android.
- Outcome: API 1.3 (`Conversation.activity_ms` and `last_message`; `conversations.list` by activity, in pages with an opaque cursor); the interface reads the conversations in pages of 50, merges a person's conversations into one entry (`inboxEntries`) placed by the most recent one, virtualizes the inbox with virtua (rows carry `aria-setsize` and `aria-posinset`), heads a person's thread with their name and a switcher between their conversations, and shows the inbox or the thread alone on narrow screens, where Back returns to the inbox (ADR 0021). The Android app now loads the interface (`npm run sync` copies `ui/dist` into `www/`; the test page of T1.3 is gone) and requires Android System WebView 111 or later, showing a page asking to update it otherwise. The echo network gained a group, "crowd" logins (120 more contacts) and "offline" logins, which made the refusal to link a group testable. Verified: the Playwright journeys on Windows (order and previews, merged person, pages while scrolling, offline account, narrow window), the update page on the API 30 emulator (WebView 83), the whole Android journey on that emulator with a build lowered for WebView 83; the real build's journey runs on CI's API 35 emulator. Found and fixed: the store overwrote events received during a reload with the older state it read, and a thread's message list and composer shared a React key (a sent message showed three times). Left: unread counts (the core has no read state yet), search in the inbox, TalkBack and NVDA checks.

**T2.6 — Signal on the device** 📞
- Deliverable: spike, then connector: libsignal (Rust, static C library) built for Windows, Linux, macOS and Android; mautrix-signal's connector in the core; a test account on Signal's staging servers, registered by the maintainer with a real phone number (ADR 0005).
- 🧪 Test: automated journey against staging, like Telegram's.
- Outcome of the spike (ADR 0022): feasible with reservations. mautrix-signal v0.2609.0's libsignal (`libsignal-ffi` v0.102.2) builds on GitHub's runners for Linux, macOS, Windows (Rust's GNU toolchain as the host, linked by MSYS2 UCRT64) and the four Android ABIs (the NDK, with libsignal's own Android settings); libsignalgo's Go tests pass on Linux, macOS and Windows, and link for Android (not run). Windows needed UCRT64 first in `PATH`, `-L` to the x86-64 `libwindows`, and a one-line patch of BoringSSL's P-256 assembly guard. Build times 3½ to 12 minutes per target, so CI will cache libsignal; mautrix-signal's whole bridge is 37 to 61 MB stripped. The core gets the connector behind a `musubee_signal` build tag (cgo), so builds without it stay pure Go. Staging is reachable with a patch of signalmeow (hosts, CDSI measurement, zkgroup parameters; the TLS roots already match). Found: signalmeow does not validate sender certificates (impact unknown; to analyse before users get Signal). The workflow `.github/workflows/libsignal-spike.yml` repeats the spike by hand. Left to the maintainer: the test environment (staging with their own number), downloads for the connector work (protoc, signal-cli, the Go dependency), a second number lent for free, the form of the staging patch (upstream pull request), and reporting the certificate observation.
- Outcome of the connector (ADR 0025): the maintainer chose staging with their own number, a copy of mautrix-signal in our repository, and one account for now. mautrix-signal v0.2609.0 is copied into `core/replace/mautrix-signal`, patched so that `signalmeow.SetEnvironment` points it at production or staging (hosts, gRPC target, contact discovery enclave of libsignal v0.102.2, zkgroup parameters). The core offers Signal (QR code login, device name "Musubee") when built with the `musubee_signal` tag and configured with `signal`; without the tag it refuses that configuration. CI builds libsignal-ffi with `scripts/build-libsignal.sh` on Linux, Windows and macOS, caches it, and runs the copy's tests and the core's Signal tests. Signal adds 40 MB to the Windows core (96.3 MB stripped). The sender certificate analysis is done; the maintainer reports it privately to upstream, and its details stay out of the repository until upstream fixes it or 90 days pass. Verified on Windows: the Go tests with and without the tag, the license audit, and the staging journey (`infra/signaltest`, signal-cli as the phone, the maintainer's number registered on staging): linked in 9.6 s, a Note to Self each way, no message text in the core's log. Left, before any app ships Signal: sealing signalmeow's tables, which hold the account's keys, sessions and buffered message plaintext in clear (ADR 0019); libsignal for Android on CI and the connector in the Android and desktop apps; the journey on CI.

**T2.7 — Read state and unread counts** (E2.3)
- Deliverable: what T2.5 left: an unread count per conversation, cleared by reading in Musubee or on the network's own app, and passed to the networks that support read receipts.
- 🧪 Test: the core's storage and API tests, the interface's unit tests, and the desktop Playwright journey.
- Outcome: the local Matrix storage keeps the user's read position per room (schema v3, `local_read_marker`), which only moves forward; unread = messages of others after the later of that position and the user's own last message, up to 1000 (ADR 0023). API 1.4: `Conversation.unread_count` and `marked_unread`, `conversations.markRead`, and a `conversation.updated` after each `message.added`. Reading in Musubee queues a Matrix read receipt to the room's bridge, so bridgev2 records it and calls the connector (echo in the tests; Telegram's handler is read, not run); the user's reads reported by a network arrive through the double puppet. The inbox shows a badge (the count, "99+", or a dot for "marked unread"), summed for a person, described to screen readers as "N unread messages"; the thread marks a conversation read when its newest message is in view in a visible window, at once in the store. Verified on Windows: Go tests (with `-race`), Vitest, the desktop Playwright journeys. Left: marking unread from the interface, read receipts of native Matrix accounts, others' "seen" in the thread (E2.4), the cost of the counts on a low-end phone, NVDA and TalkBack.

**T2.8 — Merging conversations in the interface** (E2.5)
- Deliverable: what T2.1 and T2.5 left: the user merges the same person's conversations from the interface, renames the person, and separates them again.
- 🧪 Test: unit tests of the suggestions and of the store, and a desktop Playwright journey with the real core.
- Outcome: a "Same person…" button in a direct conversation's header opens a panel listing the other direct conversations, those with a similar name first ("Alice" and "Alice T."), with a search ignoring case and accents; "Merge" links one into the thread's person, bringing along the other's person if it has one, or creates a person (ADR 0024). Once merged, the same panel ("Person…") renames the person, separates the conversation read, or separates them all after a confirmation. Verified on Windows: Vitest, and the Playwright journey (merge, rename, separate all). Left: a people screen, related groups, suggestions from phone numbers or usernames, and the panel on Android, assumed from the shared interface.

**T2.9 — Signal's secrets at rest** (E2.9)
- Deliverable: what T2.6 left before any app ships Signal: signalmeow's tables, which hold the account's keys, the protocol sessions and buffered message plaintext in clear, sealed with ADR 0019's master key; ADR.
- Acceptance: no Signal key, session or message content is readable in the database file.
- 🧪 Test: the copy's store on a real SQLite database with real libsignal objects, the core's sealer, and the staging journey checking `core.db`.
- Outcome: signalmeow's store, in our copy, seals its nineteen secret columns (the device's keys and password, sessions, pre-keys, sender keys, profile keys, group master keys, the event buffer and the backup tables) in Go, before each query and when each row is scanned, with a sealer per store (change 2 of `MUSUBEE.md`, ADR 0026); with a sealer, a value that does not open is an error. The core requires the master key for Signal and seals with AES-256-GCM, one derived key per column (`signal/` purposes). ADR 0019's driver was not extended: signalmeow's generic column names would need a SQL parser on every read. Names and phone numbers stay in clear, like the rest of the core's personal data. Verified on Windows: the copy's tests (every column sealed, read back, refused in clear; the test caught a column unsealed by hand), the core's tests with and without Signal, lint, and the staging journey (every sealed column of `core.db` holds only sealed values). Left before an app ships Signal: the sender certificate outcome (ADR 0025), libsignal for Android on CI, the connector in the apps.

**T2.10 — The core's key on macOS and Linux** (E2.9)
- Deliverable: ADR 0019's follow-up: the master key of the secrets at rest in the system's secure storage on macOS and Linux, instead of a plain file.
- 🧪 Test: the core program's tests, unit tests of the app's key handling, the supervisor with the real core, and the desktop Playwright journeys on CI with the macOS Keychain and a GNOME keyring.
- Outcome: the desktop app keeps the key in `core.key` encrypted by Electron `safeStorage` (synchronous API, protection `electron-safestorage`) and gives it to the core on the first line of its standard input (`musubee-core -key-from-stdin`), never on its command line or in its environment (ADR 0027). A plain key file moves into `safeStorage` with the same key; a key of `safeStorage` that does not open stops the core; the core alone refuses such a file. Windows keeps DPAPI; a Linux desktop without a password store (`basic_text`) keeps the plain file, with a warning. Verified on Windows: the Go tests, Vitest with the real core, and the desktop Playwright journeys (DPAPI). Verified on CI: the macOS Keychain and the GNOME keyring, once the app removed the `--password-store=basic` and `--use-mock-keychain` switches that Playwright adds to every Electron app it starts (the first runs had checked a mock Keychain and a hardcoded password). Left: the asynchronous API when it can report its fallback, a warning in the interface, the Keychain prompts of a signed app (E3.2).

**T2.11 — libsignal for Android on CI** (E2.9)
- Deliverable: what ADR 0025 and ADR 0026 left before the Android app gets Signal: libsignal-ffi built for Android by the repository's build script and cached on CI, and the Signal code tested on Android, not only linked; ADR.
- Acceptance: the tests of the mautrix-signal copy and of the core's Signal code pass on an Android emulator; arm64-v8a links; no binary needs `libc++_shared.so`.
- 🧪 Test: the test binaries, built with `GOOS=android`, run on CI's x86_64 emulator with adb.
- Outcome: `scripts/build-libsignal.sh` takes an Android ABI and builds libsignal-ffi with the settings of ADR 0022, on Linux, macOS or Windows hosts; for Android it adds a `libc++.a` linker script, because the NDK's clang turned libsignalgo's `-lstdc++` into a dependency on `libc++_shared.so`, which devices lack (caught by the new job's check). `scripts/run-android-go-tests.sh` runs Go test binaries on a device with adb. CI's `signal-android` job builds and caches libsignal for x86_64 and arm64-v8a, runs the copy's tests and the core's Signal tests on an Android 11 x86_64 emulator (7 binaries, all passing), links arm64-v8a, and measures the core: Signal adds about 20 MB to the stripped Android core (79.3 MB on x86_64, 74.5 MB on arm64-v8a) (ADR 0028). Verified on Windows: the x86_64 build and the links of Signal's test binaries, with `clang.exe --target=...` as Go's C compiler (the NDK's `.cmd` wrapper overflows cmd.exe's command line, which Gradle will need to handle). Left: the connector in the Android app (Gradle, the instrumented test), the tests on an arm64 phone 📞, armeabi-v7a and x86 on CI.

**T2.12 — Signal in the Android app** (E2.9)
- Deliverable: the connector in the Android app, which ADR 0028 left: the core built with Signal by Gradle when asked, the configuration from the build, an instrumented test, CI; ADR.
- Acceptance: a build with Signal offers it and its QR login reaches Signal's staging service on an emulator; builds without it are unchanged; no build with Signal by default.
- 🧪 Test: `SignalTest` (instrumented) on CI's API 30 and 35 emulators, on a build with Signal on staging; the other instrumented tests and the Appium journey on the same build.
- Outcome: the Gradle properties `musubee.signal` (`staging` or `production`) and `musubee.libsignal` (libsignal-ffi per ABI from `scripts/build-libsignal.sh`) build the core with the `musubee_signal` tag and put the service in `BuildConfig`; the core service passes it to the core (ADR 0029). On Windows, Gradle gives Go `clang.exe --target=...` instead of the NDK's `.cmd` wrapper. CI's Android app job builds with Signal on staging from the libsignal cache of T2.11's job. Verified on Windows with the API 30 x86 emulator: 13 instrumented tests pass with Signal (linking code from staging in 339 to 368 ms) and without it (`SignalTest` skipped); on CI, `SignalTest` passes on the API 30 and 35 emulators with the Appium journey on the same build; the core grows from 54.6 to 74.6 MB on x86. Found: the build script made `llvm-ar.exe.exe` on Windows hosts (fixed in T2.11's branch); linking from the phone that runs Signal is an open question, and the interface does not draw QR codes (E2.2). On a Pixel 8 Pro (Android 17, 2026-10-12): the arm64-v8a build passes the 13 instrumented tests with Signal required (linking code in 363 to 412 ms) and the Appium journey, after the inbox's rows named themselves with `aria-label`, because WebView 153 leaves names from `aria-labelledby` out of Android's accessibility tree (ADR 0029). Left: the sender certificate outcome before users get Signal, memory and battery with an account connected, what TalkBack reads on WebView 153.

**T2.13 — QR codes in the login steps** (E2.2)
- Deliverable: what T2.12 found missing: the interface draws a login step's QR code (a display of type `qr`: Signal's and Telegram's linking codes) instead of printing its text; the echo network gets a QR code flow to test it without a phone.
- Acceptance: the image that the app shows decodes to the step's data; the data stays readable as text; the Content-Security-Policy is unchanged.
- 🧪 Test: unit tests that draw the QR code and decode it with jsQR; the core's login test of the echo QR code flow; a desktop Playwright journey that decodes the image the app shows, then sees the account added.
- Outcome: `ui/src/qr.ts` encodes with uqr 0.1.3 (MIT, a port of Project Nayuki's generator; error correction M, a quiet zone of 4 modules) into an SVG document; `QrCode` shows it as an `<img>` with a `data:` URI (allowed by `img-src data:`), black on white in every theme, at most 320 px wide, with the data under "Show the code as text". The echo network's "QR code" flow shows `musubee-echo:` and a code, and completes by itself after the echo delay. Verified on Windows: the UI and core tests, and the desktop journeys (the shown image decodes to the step's data). Unknown: scanning the code shown by the app with Signal's or Telegram's phone app (it needs a second device: the phone that runs the network's app cannot scan its own screen), and the display on the Pixel (disconnected during the task). Left: linking from the phone that runs Signal (E2.2).

Epics:
- **E2.1** Domain model (`Account`, `Conversation`, `Message`, `Person`) and abstraction layer, with no Matrix type leaking to the UI.
- **E2.2** Onboarding without Matrix jargon, account management, network connection flows (QR, codes).
- **E2.3** Unified inbox, conversation list, pagination, offline.
- **E2.4** Conversation thread: text, replies, reactions, editing; media, voice messages, GIFs, files.
- **E2.5** **Conversation merging** per person and easy switching between threads.
- **E2.6** Android notifications (foreground service, FCM/UnifiedPush). T1.6 found that the core's Telegram connection does not stay up in deep Doze (ADR 0014, Battery): push must wake the app, then the core reconnects; measure the delivery delay and the cost of holding the connection.
- **E2.7** Customization: design tokens, themes per app, per conversation, per person.
- **E2.8** Encryption: honest indicators of the connection mode (on-device / hosted bridge), key backup and recovery.
- **E2.9** Secrets at rest: the core's database (network session keys, such as Telegram's, ADR 0014) encrypted with a key held in the OS secure storage (Android Keystore, Windows DPAPI or Credential Manager, macOS and iOS Keychain, Linux Secret Service). Required before any public release (`CLAUDE.md` §6.8).

## Phase 3 — Self-hosting, full desktop, iOS

- **E3.1** Hosted-bridge mode: Docker package (homeserver + bridges + push relay), self-hosting documentation.
- **E3.2** macOS and Linux (installers, signing, updates).
- **E3.3** iOS 🍎 💶: hosted mode first, then on-device if T1.7 allows it. Notification extension, TestFlight for the test user.
- **E3.4** Push relay (receiving native pushes, encrypted forwarding to the apps).

## Phase 4 — Extension

- **E4.1** WhatsApp (legal risk to be reassessed with a competent human).
- **E4.2** Other networks, native calls between Musubee users (MatrixRTC).
- **E4.3** Extension system / advanced customization.
- **E4.4** "Hosted core" web app.
