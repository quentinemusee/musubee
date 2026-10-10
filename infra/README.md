# infra — realistic test environment

Services started with Docker for integration and end-to-end tests. Principle: **test against the real thing**, never against mocks (see [`docs/TESTING.md`](../docs/TESTING.md)). Design and measurements: [ADR 0004](../docs/ADR/0004-integration-test-environment.md).

## Contents

| Path | Purpose |
|---|---|
| `compose.test.yml` | Synapse v1.162.0 + PostgreSQL 18.6, pinned by digest |
| `synapse/` | Test-only Synapse configuration (no federation, rate limits lifted) |
| `testenv/` | Go package that starts and stops the environment and creates users; integration tests |
| `cmd/testenv/` | Command for manual work: `up`, `status`, `user`, `down` |
| `compose.telegram.yml`, `telegram/` | Official mautrix-telegram image and its per-run configuration templates ([ADR 0006](../docs/ADR/0006-telegram-e2e-with-test-bots.md)) |
| `telegramtest/` | Bot API client and the Telegram end-to-end tests of the hosted bridge |
| `coretest/` | Helpers of the end-to-end tests that drive the core through its API: commands, the event stream, and the check that no Matrix identifier reaches the API |
| `matrixtest/` | The Matrix account journey of the core against Synapse: sign-in, an encrypted direct conversation, a restart, logout (T2.2, [ADR 0018](../docs/ADR/0018-native-matrix-accounts.md)) |
| `telegramtest/ondevice/` | The Telegram end-to-end test of the core itself, on the device, without Synapse (T1.6, [ADR 0014](../docs/ADR/0014-telegram-on-device.md)) |

Requirements: Docker with the Compose plugin (Docker Desktop on Windows and macOS), Go 1.27.1 or later (version in `go.mod`; 1.27.0 has a `database/sql` deadlock, see ADR 0009).

## Run the tests

From the repository root:

```
go test -tags=goolm ./infra/...              # unit tests, no Docker needed
go test -tags=goolm,integration ./infra/...  # integration tests: starts a fresh environment (about 12 s), then removes it
```

The `goolm` build tag (pure-Go Olm) is needed by every package that imports the core (ADR 0018). Without Docker the integration tests are skipped; set `MUSUBEE_REQUIRE_INTEGRATION=1` to make them fail instead (CI does).

## Telegram end-to-end tests

They run the official mautrix-telegram bridge with Synapse, on production Telegram, using two **dedicated test bots** and no user account ([ADR 0006](../docs/ADR/0006-telegram-e2e-with-test-bots.md)):

```
go test -tags=telegram -v ./infra/telegramtest/
```

`TestBridgeOffersLogin` needs nothing else. `TestMessageFlowsThroughTheBridge` (the T0.4 acceptance test) needs, once:

1. Two bots created with **@BotFather** (`/newbot`): the **bridge bot**, which the bridge logs in as, and the **peer bot**, which plays the remote party.
2. A **private channel** (not a group: Telegram never delivers messages between bots in groups) with both bots as **administrators** allowed to post.
3. The tokens as repository secrets (each command asks for the value):
   ```
   gh secret set MUSUBEE_TG_BRIDGE_BOT_TOKEN
   gh secret set MUSUBEE_TG_PEER_BOT_TOKEN
   ```
4. The channel ID: the test finds it in the peer bot's updates of the last 24 hours (post any message in the channel) and prints it; store it as a repository variable so later runs do not depend on that window: `gh variable set MUSUBEE_TG_CHAT_ID`.

5. The project's own application credentials, created on <https://my.telegram.org> ("API development tools"): `gh secret set MUSUBEE_TG_API_ID` and `gh secret set MUSUBEE_TG_API_HASH`. Without them the test falls back on the public test application, which Telegram rate-limits for everyone (`API_ID_PUBLISHED_FLOOD`).

For local runs, export the same variables. In CI the tests have their own workflow (`.github/workflows/telegram.yml`): it runs when `infra/`, `core/`, `go.work` or the Telegram tests of the apps change, on `master`, once a day, and on demand (`gh workflow run telegram`), because Telegram rate-limits bot logins (`FLOOD_WAIT`).

### On the device (T1.6)

The same bots, channel and application credentials drive the Telegram connector inside the core, with no bridge and no Synapse ([ADR 0014](../docs/ADR/0014-telegram-on-device.md)):

```
go test -tags=goolm,telegram -v ./infra/telegramtest/ondevice/
```

It logs in as the bridge bot through the core API, receives a post of the peer bot, answers it, checks that the peer bot sees the answer, logs out, and checks that the core's log holds neither the messages nor the token. Without the variables it is skipped; `MUSUBEE_REQUIRE_TELEGRAM_E2E=1` makes it fail instead (CI does). The workflow then runs the same journey in the desktop app (`apps/desktop/e2e/telegram.spec.ts`, Windows) and in the Android app on an emulator (`TelegramTest`), one after the other: **a bot has one session at a time, so never run two Telegram tests at once**, and every test logs out at the end.

`infra/go.mod` requires the core module (through a `replace` to `../core`) and repeats the core's `replace go.mau.fi/webp => ../core/replace/webp`, which Go does not inherit.

## Use the environment by hand

```
go run ./infra/cmd/testenv up                        # prints the homeserver URL (random localhost port)
go run ./infra/cmd/testenv user alice some-password  # creates @alice:musubee.test
go run ./infra/cmd/testenv down                      # removes containers, data and secret
```

Any Matrix client can log in to the printed URL with the users you create. `-project NAME` runs several environments side by side.

## Rules

- **No secrets and no real accounts in this directory.** The registration shared secret is generated at random for every run; the developer command keeps it in `infra/.testenv/`, which Git ignores.
- Everything is throw-away: data lives in memory (tmpfs) and disappears on `down`.
- Images are pinned by digest; update tag and digest together.

## Not included yet

- **Echo connector** (ours, T1.1), replacing Beeper's dummybridge, which has no license (ADR 0004).
- Fault-injection proxy (for example Toxiproxy): when the core needs it.
