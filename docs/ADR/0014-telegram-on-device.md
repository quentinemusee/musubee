# ADR 0014 — Telegram on the device: mautrix-telegram's connector inside the core

- **Status**: accepted
- **Date**: 2026-10-09
- **Task**: T1.6
- **Deciders**: Quentin Raimbaud (maintainer), with the design assistant

## Context

T1.6 delivers the first real network in "on-device" mode (`CLAUDE.md` §2): the core talks to Telegram's servers itself, so no server of ours, and no Matrix homeserver, sees the messages. ADR 0009 showed that a `bridgev2` connector runs in-process on our local implementation of the Matrix side (`core/localmatrix`); ADR 0011 put the core in an Android foreground service; ADR 0013 put it in a child process of the desktop app. T1.6 must show the same with Telegram, tested end to end on Windows and on an emulated Android, and then measure on a real phone what an idle Telegram connection costs in battery.

Constraints:

- **License**: everything we ship is AGPL-3.0-or-later; dependencies must be compatible (ADR 0008).
- **Pure Go core** (ADR 0009, 0010): the desktop and iOS builds use no cgo, and Android uses cgo only for its SQLite driver (ADR 0011).
- **No secret in the repository or in logs** (`CLAUDE.md` §6.3, §6.8): Telegram's application credentials (`api_id`, `api_hash`) and the test bots' tokens come from the environment.
- **No real user account in CI** (`CLAUDE.md` §6.3): the tests use the two test bots and the private channel of ADR 0006.
- **No Matrix type crosses the API** (ADR 0012).

## Options considered

### Which Telegram implementation

#### A. mautrix-telegram's network connector, as a library (chosen)

`go.mau.fi/mautrix-telegram` v0.2609.0 (commit `ee6fb0f8`, 2026-09-16) is the Go rewrite of the Telegram bridge on `bridgev2`. Its package `pkg/connector` is the network connector; it brings a fork of gotd/td (MTProto in pure Go) in `pkg/gotd`.

- Pros: the connector T0.4 already tested as a hosted bridge (ADR 0006), now in-process. Same mautrix-go (v0.31.0) and go-util (v0.10.1) as ours, so no version conflict. Login flows (phone, QR code, bot token) are `bridgev2` login processes, which the API already carries (ADR 0012). AGPL-3.0-or-later, like us.
- Cons: a large dependency (see Measurements: the core grows by about 36 MB). It is written for a server: commands, external converters for stickers, a cgo WebP encoder. Its `LICENSE.exceptions` grants exceptions to Beeper and Element only: we take it under the plain AGPL, which is what we need.

#### B. gotd/td directly, with our own connector

- Pro: we would control the size and the features.
- Cons: re-implementing what mautrix-telegram does (login flows, portals, backfill, media, reactions...). Months of work for no gain in T1.6.

#### C. TDLib (Telegram's official C++ library)

- Cons: C++ and a JSON interface of its own: cgo or a separate process on every platform, which ADR 0009 and 0010 avoid. It would not be a `bridgev2` connector, so the whole local Matrix layer would have to be bypassed.

### The cgo WebP encoder

mautrix-telegram imports `go.mau.fi/webp` (libwebp through cgo) to convert PNG and JPEG stickers to WebP before sending them. It is the connector's only cgo package.

#### A. A pure-Go stand-in through a `replace` directive (chosen)

`core/replace/webp` is a module `go.mau.fi/webp` of our own whose `Encode` returns `ErrUnsupported`; `core/go.mod` replaces the real module with it.

- Pros: the core stays pure Go on every platform. Nothing else changes in the connector.
- Cons: sending a PNG or JPEG **sticker** to Telegram fails with an error (ordinary images are not converted and are not affected). A `replace` directive is not inherited by modules that import ours: `infra/go.mod` repeats it.

#### B. Keep libwebp (cgo)

- Con: cgo and a C library on the desktop and iOS builds, against ADR 0009 and 0010, for one sticker case.

#### C. Ask upstream for a build tag that leaves the encoder out

- Pro: the cleanest end state.
- Con: an external request, to be made by the maintainer if they wish; option A works meanwhile and is easy to drop.

### The application credentials

Telegram identifies each client application by an `api_id` and an `api_hash` (https://my.telegram.org). Every Telegram client ships them inside the app; Telegram asks developers to obtain their own rather than reuse another client's.

#### A. From the environment of the build or of the process (chosen)

- Desktop: `musubee-core` reads `MUSUBEE_TG_API_ID` and `MUSUBEE_TG_API_HASH` from its environment (not flags, which other processes can read). The desktop app passes its environment to the core.
- Android: Gradle reads the same variables (or the properties `musubee.telegramApiId` and `musubee.telegramApiHash`) into `BuildConfig`; the core service passes them to the core.
- Without them, the core does not offer Telegram.
- Pros: nothing in the repository; forks build without Telegram until they get their own credentials.
- Con: release builds (E3.2) must inject them, and the app carries them like every Telegram client: they identify the application, they are not a user secret.

#### B. Committed to the repository

- Con: forks would reuse our identity towards Telegram, and Telegram can revoke credentials that are abused.

## Decision

1. **The core embeds mautrix-telegram's connector** (`core/connector/telegram`), wrapped to set the upstream defaults, the credentials, the device model "Musubee", and no animated-sticker conversion (it needs `lottieconverter` and `ffmpeg`, which devices do not have).
2. **The "manual" login flow is not offered**: it imports raw session credentials and upstream marks it "advanced, do not use". Phone number, QR code and bot token remain.
3. **The WebP encoder is replaced by a pure-Go stand-in** (option A above). A test checks that the stand-in is what the connector links.
4. **bridgehost uses bridgev2's real command processor** (`commands.NewProcessor`): the connector adds its handlers to it in `Init` and requires its concrete type. Commands stay unreachable: the command prefix is a string no message starts with, and the user has no `Commands` permission, so bridgev2 refuses a command before any handler runs.
5. **`accounts.logout` (API 1.1)** logs an account out of its network and removes it, with the conversations only it reached, before the command returns. A real network keeps a session on its servers: the user must be able to end it, and the tests must not leave sessions of the bots behind (one session per bot at a time, ADR 0006). After bad credentials the conversations stay, for the user to log in again.
6. **The credentials come from the environment** (option A above).
7. **End-to-end tests with the test bots** run the same journey three times: log in as the bridge bot, receive a post of the peer bot in the private channel, answer it, check that the peer bot sees the answer, log out. In the core alone through its API (`infra/telegramtest/ondevice`, which also checks that `core.log` holds neither the messages nor the token), in the desktop app through its interface (`apps/desktop/e2e/telegram.spec.ts`, Windows), and in the Android app's core service on an emulator (`TelegramTest.kt`). The `telegram` workflow runs them one after the other, after the bridge test of T0.4.

## Measurements

### Size

Stripped builds (`-trimpath -ldflags="-s -w"`), Go 1.27.1, measured on 2026-10-09 on the maintainer's machine:

| Build | Before T1.6 (master) | With Telegram | Growth |
|---|---|---|---|
| Desktop `musubee-core.exe` (Windows amd64, `CGO_ENABLED=0`) | 18.0 MB | 54.1 MB | ×3.0 |
| Android `libmusubee.so` (x86_64, ADR 0011) | 17.0 MB | 55.9 MB | ×3.3 |
| Android debug APK (x86_64 only) | | 60.1 MB | |

`go tool nm -size` attributes 12.2 MB of symbols to gotd's `tg` package (the generated Telegram schema), the largest contributor identified; the rest of the growth is spread over the connector, gotd's other packages and their dependencies (assumed from the symbol list, not broken down further).

### End-to-end, against production Telegram

The `telegram` workflow, run 37908453046 on 2026-10-09 (GitHub-hosted runners; one run, so the figures are an order of magnitude, not a distribution):

| Step | Core alone (Windows) | Android emulator (API 35, x86_64) |
|---|---|---|
| Bot login (`login.start` to `complete`) | 5.7 s | 10.8 s |
| Account connected, from the start of the login | 6.1 s | 11.2 s |
| Peer bot's post → `message.added` in the core | 3.0 s (first attempt) | 5.3 s (first attempt) |
| `messages.send` → status `sent` | 161 ms | 168 ms |
| `messages.send` → seen by the peer bot | 258 ms | 296 ms |
| Go heap after GC, in use / from the system | 2.1 / 6.6 MiB | 1.9 / 7.3 MiB |
| Goroutines | 30 | 29 |

The desktop journey through the interface (Playwright, Windows) took 15.8 s from launch to logout. Most of the login time is the bot login itself: the connector first calls the Bot API's `logOut` for the token, then authorizes over MTProto. The delay before a post reaches the core is Telegram's: a bot receives channel posts as updates pushed by the server.

The Go heap stays small; the process's resident memory (the 56 MB library mapped, the runtime) was not measured on the emulator in this task.

### Battery

**Pending**: one hour on a real phone left idle, connected to Telegram (`docs/TASKS.md`, T1.6). To be added here.

## Known gaps

- **Telegram session keys are stored in plaintext.** mautrix-telegram keeps the MTProto authorization key in the login's metadata (`UserLoginMetadata.Session.AuthKey`), which `bridgev2` stores as JSON in the `user_login` table of `core.db`. The file is in the app's private data directory, but `CLAUDE.md` §6.8 asks for keys in the OS secure storage. Anyone who can read `core.db` can use the session. To fix before a public release: encrypt `core.db` (SQLCipher-like, with a key from the OS keystore) or the metadata column. This is a security debt, recorded here and in `docs/TASKS.md`.
- **Stickers**: PNG and JPEG stickers cannot be sent (WebP stand-in); animated stickers are sent and received unconverted.
- **Only the bot flow is tested automatically.** Logging in with a phone number or a QR code needs a real user account, which CI must not use (`CLAUDE.md` §6.3). The login fields (`phone_number`, `2fa_code`, `password`) and the QR display already exist in the API; a manual test with the maintainer's own account belongs to the battery test.
- **Bot tokens and messages in test logs**: the tests never print them, GitHub masks the secrets in logs, and the desktop test records no Playwright trace (a trace would contain the typed token).

## State of knowledge

**Verified** (2026-10-09):

- mautrix-telegram's connector runs in the core, without a homeserver, on Windows (`CGO_ENABLED=0`) and on Android (x86_64 emulator): bot login, receiving a post of another bot, sending an answer that the other bot sees, logging out. Three automated journeys pass against production Telegram: the core alone, the desktop app through its interface, and the Android app's core service.
- The logs: `core.log` contains neither the messages nor the bot token (checked by the core test).
- Logging out removes the account and its conversations from the device, and the core keeps nothing of them after a restart (core and desktop tests).
- The core stays pure Go on the desktop: `CGO_ENABLED=0` builds and tests pass, with the WebP stand-in.
- Without the application credentials, Telegram is not offered; with invalid ones, the login fails with Telegram's error ("The api_id/api_hash combination is invalid", seen on an API 30 emulator).
- Licenses: mautrix-telegram and every new dependency are AGPL-compatible (`scripts/licenseaudit`, NOTICE).
- Sizes, as in the table above.

**Assumed**:

- Logging in with a phone number or a QR code works as it does in mautrix-telegram's bridge: same code, not run here (needs a real user account).
- The idle cost of a Telegram connection is modest: the core keeps one MTProto connection and the server pushes updates. To be measured (battery test).
- The share of the size growth beyond the 12.2 MB of `tg`.

**Unknown**:

- Whether logging out ends the MTProto session on Telegram's side: the connector asks for it, but the tests cannot see it (a bot login logs the token out of the Bot API first anyway).
- The battery cost of the idle connection on a real phone.
- Whether the iOS notification extension can hold this code (T1.7): the size suggests a separate, much smaller build for the extension, or the hosted mode.
- How Telegram treats a client application whose users log in from many devices with the same credentials over time (rate limits, `FLOOD_WAIT`): a bot can log in only so often, and the workflow logs in four times per run.

## Consequences

- Telegram works on the device, on desktop and Android, with the credentials injected at build or start-up time. Release builds (E3.2) need them as CI secrets.
- The core is about three times larger. This weighs on downloads and, above all, on the iOS notification extension (T1.7), which may not be able to load the Telegram code at all.
- The Android test page (`apps/mobile/www`) gains a Telegram login (every flow but the QR code, which it cannot draw) for the manual tests on a real phone, until the real interface replaces it.
- `infra` now depends on `core` (for the end-to-end test), with the same `replace` directive.
- The `telegram` workflow logs the bridge bot in four times per run (bridge, core, desktop, Android). It runs only when the code involved changes, on master, daily and on demand.
- **Revisit if:** upstream offers a way to build without libwebp (drop the stand-in); the size blocks iOS (T1.7); Telegram rate-limits the bot logins of CI more strictly; the session keys get encrypted at rest.

## Sources

Accessed 2026-10-09.

- mautrix-telegram v0.2609.0 (commit `ee6fb0f87097286a1912a4767735cd87e89bddc1`): https://github.com/mautrix/telegram. Read: `pkg/connector/config.go` (`ValidateConfig`, example configuration), `login.go` (flows), `loginbot.go` (bot login, which calls the Bot API's `logOut` for the token first), `metadata.go` (`UserLoginMetadata.Session`), `LICENSE`, `LICENSE.exceptions`, `pkg/gotd/LICENSE`.
- mautrix-go v0.31.0, `bridgev2/queue.go` (commands are dispatched only for the command prefix or the management room), `bridgev2/userlogin.go` (`Delete`, `DeleteOpts`, cleanup on logout), `bridgev2/bridgeconfig/config.go` (`CleanupOnLogouts`): https://github.com/mautrix/go
- Telegram, obtaining an `api_id`: https://core.telegram.org/api/obtaining_api_id
- Telegram Bot API, `logOut`: https://core.telegram.org/bots/api#logout
- ADR 0006 (test bots), 0009 (bridgev2 in-process), 0011 (Android), 0012 (API contract), 0013 (desktop shell)
