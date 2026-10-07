# ADR 0005 — Telegram test environment for end-to-end bridge tests

- **Status**: accepted (the message test waits for the maintainer's test account, see "Consequences")
- **Date**: 2026-10-07
- **Task**: T0.4
- **Deciders**: Quentin Raimbaud (maintainer), with the design assistant

## Context

T0.4 asks for the mautrix-telegram bridge connected to the local Synapse and to Telegram's official **test environment** ("test DCs", fully separate from production), and for an automated end-to-end test, usable in CI, in which a message flows between two Telegram test accounts through the bridge. `CLAUDE.md` §6 rule 3 allows test credentials as encrypted CI secrets, never a real user account or a production secret. The maintainer asked to try the public test `api_id` first, and to fall back on their own phone number.

## Findings

1. **The public test application works.** Telegram's open-source clients ship a sample `api_id` (17349, also exported by gotd/td as `telegram.TestAppID`). With it, the bridge connects to the test DCs and obtains a QR login token (`TestBridgeOffersQRLoginOnTestServers`, run locally on 2026-10-07).
2. **`99966XYYYY` numbers no longer sign in.** Telegram's documentation still describes them (code = DC number repeated), but every attempt failed with `PHONE_CODE_INVALID` on the three test DCs, with the code length announced by the server (5) and with 6 digits. gotd/td's source carries the same warning ("as of 2026, Telegram no longer auto-provisions accounts for randomly generated 99966X test phone numbers").
3. **Accounts on the test environment are created from an official app.** Telegram's Bot API documentation, "Using bots in the test environment": on Telegram Desktop, Settings, then Shift + Alt + right click on "Add Account", then "Test Server"; a new account and a new bot (@BotFather) must be created there. The phone number is real.
4. **mautrix-telegram cannot target the test DCs.** Its configuration has no such option and it never sets gotd's DC list, so it always uses production (read in `pkg/connector/client.go` and `login.go` at commit `9b2a6e3e`).
5. **The bridge offers four login flows**: phone number, QR code, bot token, existing session.

## Decision

1. **A 23-line patch** (`infra/telegram/test-servers.patch`) adds a `test_servers` option to mautrix-telegram; when set, both Telegram clients of the bridge use `dcs.Test()`. `infra/telegram/Dockerfile` builds the bridge from a pinned commit with the patch, on digest-pinned base images. The patch stays public under AGPL-3.0-or-later, as the bridge's license requires.
2. **`infra/compose.telegram.yml`** adds the bridge to the test environment. `infra/testenv` renders the bridge configuration and the appservice registration with random per-run tokens (the bridge's own `-g` cannot rewrite its config on Docker Desktop bind mounts), and gives Synapse a per-run configuration directory with the appservice fragment.
3. **One real test account plus one test bot**, instead of two test accounts:
   - account **A** is created once by the maintainer on the test environment with their own number (the maintainer agreed; the account is separate from production Telegram);
   - bot **B** is created by A with @BotFather on the test environment, and plays the remote correspondent through the test Bot API (`https://api.telegram.org/bot<token>/test/<method>`).
4. **Fully automated login.** The harness keeps a session of A (`MUSUBEE_TG_SESSION`, created once with `go run ./infra/cmd/tgsession`). For each run it starts the bridge's QR login and accepts the token itself (`auth.acceptLoginToken`), then logs the bridge out at the end. No human action per run.
5. **The acceptance test** (`TestMessageFlowsThroughTheBridge`): A starts bot B; B writes to A; the message reaches the Matrix user through the bridge; the Matrix user replies; B receives the reply on Telegram.
6. **Secrets**: `MUSUBEE_TG_SESSION` and `MUSUBEE_TG_BOT_TOKEN` as GitHub Actions secrets and, locally, in `infra/.testenv/` (ignored by Git). `MUSUBEE_TG_API_ID` / `MUSUBEE_TG_API_HASH` are optional (the public test application is the default). CI requires the message test as soon as the session secret exists; before that it is skipped.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| Public test `api_id` 17349 is accepted by the test DCs | **verified** (QR login token obtained by the bridge; sign-in attempts reached `auth.signIn`) | local runs, 2026-10-07 |
| `99966XYYYY` numbers fail with `PHONE_CODE_INVALID` on DCs 1, 2, 3 | **verified** (4 attempts with fresh numbers) | local spike, 2026-10-07 |
| Official docs still describe the `99966XYYYY` mechanism | **verified** | <https://core.telegram.org/api/auth> |
| Test-environment accounts are created from an official app; bots via @BotFather there; Bot API test URL format | **verified** | <https://core.telegram.org/bots/webapps#using-bots-in-the-test-environment> |
| mautrix-telegram has no test-DC option; the patch builds and the bridge runs with it | **verified** (code reading, Docker build, bridge start and QR token) | commit `9b2a6e3e`, local runs |
| With `test_servers: true`, the bridge talks to the test DCs and not to production | **verified in the code** (`DCList: dcs.Test()` in both clients); **end-to-end confirmation pending** the message test with the test account | patch |
| A session can accept the bridge's QR login token (`auth.acceptLoginToken`) without a human | **assumed** (standard MTProto login flow); **unknown** until the first run with the test account | — |
| A bot of the test environment can write to the account after `/start`, and the bridge relays both ways | **assumed**; **unknown** until the first run | — |
| mautrix-telegram is AGPL-3.0-or-later; its extra license exceptions only cover Beeper and Element | **verified** (`LICENSE`, `LICENSE.exceptions`) | repository at `9b2a6e3e` |
| Go dependencies added (gotd/td MIT and its dependencies: MIT, BSD-2/3-Clause, Apache-2.0, ISC; golang.org/x/term BSD-3-Clause) are AGPL-compatible | **verified** (license files in the module cache) | `go list -deps`, 2026-10-07 |

### Signal (as requested by T0.4)

- Signal runs a **staging environment**; signal-cli can target it (`--service-environment staging`) — **verified** in signal-cli's manual. Registration needs a **real phone number** (SMS or voice) and may require a **captcha** — **verified** in signal-cli's README. There is no equivalent of Telegram's test numbers.
- Whether mautrix-signal can target staging is **unknown** (to read in its code before Signal work starts). A CI setup would follow the same pattern as Telegram: one manually registered staging account, its state stored as an encrypted secret.

## Consequences

- **Pending on the maintainer** (about 10 minutes, once): create account A on the Telegram test environment, create bot B with @BotFather there, run `go run ./infra/cmd/tgsession`, and store the two secrets. Steps in `infra/README.md`. Until then the message test is skipped and T0.4's acceptance criterion is **not yet met**.
- The CI Telegram job builds the bridge on every run (about 4 minutes) and depends on Telegram's test environment being up; a failure there is not necessarily ours.
- The test environment is shared and periodically wiped by Telegram: account A or bot B may disappear; the fix is to recreate them and refresh the secrets.
- Proposing the `test_servers` option upstream to mautrix-telegram would remove our patch; this is an outward action left to the maintainer's decision.

## Sources

- Telegram, "User authorization", test accounts: <https://core.telegram.org/api/auth> (accessed 2026-10-07).
- Telegram, "Obtaining api_id": <https://core.telegram.org/api/obtaining_api_id> (accessed 2026-10-07).
- Telegram Bot API, "Using bots in the test environment": <https://core.telegram.org/bots/webapps#using-bots-in-the-test-environment> (accessed 2026-10-07).
- mautrix-telegram at commit `9b2a6e3ea348b37ff0076db5473e08acb71b0767`: <https://github.com/mautrix/telegram>.
- gotd/td v0.162.0 (`telegram/auth/flow.go`, `telegram/cfg.go`, `telegram/dcs/test.go`).
- signal-cli README and manual: <https://github.com/AsamK/signal-cli> (accessed 2026-10-07).
