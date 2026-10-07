# infra — realistic test environment

Services started with Docker for integration and end-to-end tests. Principle: **test against the real thing**, never against mocks (see [`docs/TESTING.md`](../docs/TESTING.md)). Design and measurements: [ADR 0004](../docs/ADR/0004-integration-test-environment.md).

## Contents

| Path | Purpose |
|---|---|
| `compose.test.yml` | Synapse v1.162.0 + PostgreSQL 18.6, pinned by digest |
| `synapse/` | Test-only Synapse configuration (no federation, rate limits lifted) |
| `testenv/` | Go package that starts and stops the environment and creates users; integration tests |
| `cmd/testenv/` | Command for manual work: `up`, `status`, `user`, `down` |
| `compose.telegram.yml`, `telegram/` | mautrix-telegram, patched to use Telegram's **test** environment ([ADR 0005](../docs/ADR/0005-telegram-test-environment.md)) |
| `telegramtest/` | Telegram test-environment client and the Telegram end-to-end tests |
| `cmd/tgsession/` | One-time login of the Telegram test account, to create its session |

Requirements: Docker with the Compose plugin (Docker Desktop on Windows and macOS), Go (version in `go.mod`).

## Run the tests

From the repository root:

```
go test ./infra/...                          # unit tests, no Docker needed
go test -tags=integration ./infra/...        # integration tests: starts a fresh environment (about 12 s), then removes it
```

Without Docker the integration tests are skipped; set `MUSUBEE_REQUIRE_INTEGRATION=1` to make them fail instead (CI does).

## Telegram end-to-end tests

They run the bridge against Telegram's **test environment** (separate from production Telegram):

```
go test -tags=telegram -v ./infra/telegramtest/      # builds the patched bridge (a few minutes the first time)
```

`TestBridgeOffersQRLoginOnTestServers` needs nothing else. `TestMessageFlowsThroughTheBridge` (the T0.4 acceptance test) needs a test account and a test bot, created once:

1. **Create the test account and save its session.** From the repository root: `go run ./infra/cmd/tgsession`. Enter your phone number (international format). The tool shows how Telegram says it sends the code (SMS, phone call, another app, payment required...); if nothing arrives, type `resend` after the delay it shows to get a phone call instead. If the number has no account on the test environment yet, the tool creates one (it asks for a first name). The session goes to `infra/.testenv/telegram/session.b64` (ignored by Git; it gives full access to the test account, keep it private). This account lives only on the test environment, not in your usual Telegram, and Telegram wipes it from time to time.
   Alternatives with an official client: Telegram Web at `https://web.telegram.org/k/?test=1`; Telegram Desktop (Settings, then **Shift + Alt + right click** on "Add Account", **"Test Server"**); iOS (tap the Settings icon 10 times, Accounts, Login to another account, Test).
2. **Create the test bot.** From that test account, talk to **@BotFather**, send `/newbot`, and keep the token it gives you.
3. **Store the secrets** for CI (GitHub CLI, from the repository root):
   ```
   gh secret set MUSUBEE_TG_SESSION < infra/.testenv/telegram/session.b64
   gh secret set MUSUBEE_TG_BOT_TOKEN
   ```
   (the second command asks for the bot token). For local runs, set the same two environment variables.

Optional: `MUSUBEE_TG_API_ID` and `MUSUBEE_TG_API_HASH` select your own application instead of the public test one.

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
