# ADR 0006 — Telegram end-to-end tests with dedicated test bots

- **Status**: accepted; supersedes the decision of [ADR 0005](0005-telegram-test-environment.md) (its findings stay valid)
- **Date**: 2026-10-07
- **Task**: T0.4
- **Deciders**: Quentin Raimbaud (maintainer), with the design assistant

## Context

ADR 0005 planned the Telegram end-to-end test on Telegram's test environment, with one test account created by hand. That turned out to be impossible:

- the `99966XYYYY` numbers no longer sign in (ADR 0005);
- the maintainer's number already has an account on the test environment, held by a session we do not control: Telegram sends the login code only there ("code sent as a message in another Telegram app") and refuses any other delivery method (`SEND_CODE_UNAVAILABLE`, verified with `tgsession` on 2026-10-07);
- signing up through Telegram Web's test mode (`web.telegram.org/k/?test=1`) delivered no code either.

`CLAUDE.md` §6 rule 3 forbids real user accounts and production secrets in CI. The maintainer offered to create bots with their personal account.

## Options considered

1. **A dedicated phone number** (prepaid SIM) for a fresh test-environment account: costs money and time; SMS delivery to real numbers on the test environment is unverified.
2. **The maintainer's personal account on production Telegram**: forbidden by rule 3; full access to private conversations would sit in CI; risk of account restrictions.
3. **Two dedicated bots and a private channel on production Telegram (chosen)**: the bridge supports logging in as a bot (`bot` login flow, verified in `pkg/connector/loginbot.go`); bots that administer a channel receive all of its posts, including those of other bots; no user account is used in CI.
4. **Reduced acceptance** (bridge start only), postponing the message test to T1.6.

## Decision

1. Two bots created by the maintainer with @BotFather: **bridge bot** (`musubee_bridge_test_bot`), which the bridge logs in as, and **peer bot** (`musubee_peer_test_bot`), which plays the remote party through the Bot API. Both administer a private channel ("Musubee CI").
2. The acceptance test (`TestMessageFlowsThroughTheBridge`): the bridge logs in as the bridge bot through the provisioning API; the peer bot posts in the channel; the post must reach the Matrix user through the bridge; the Matrix user replies; the peer bot must see the reply in the channel.
3. Secrets: `MUSUBEE_TG_BRIDGE_BOT_TOKEN` and `MUSUBEE_TG_PEER_BOT_TOKEN` (GitHub Actions secrets). The channel ID is not secret: repository variable `MUSUBEE_TG_CHAT_ID`, or found in the peer bot's recent updates.
4. **Rule 3 clarified**: tokens of bots dedicated to tests are test credentials, even on production Telegram, because they give access to no personal data. `CLAUDE.md` is updated accordingly.
5. **The official bridge image** (`dock.mau.dev/mautrix/telegram:v0.2609.0`, pinned by digest) replaces our patched build: the `test_servers` patch, its Dockerfile, the `tgsession` command and the gotd/td dependency are removed. The bridge starts in about 23 s instead of 4 minutes.
6. The CI job never runs twice at once (the bridge bot can have one bridge login at a time).

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| The official image starts, registers with Synapse, offers the `bot` and `qr` flows and gets a QR login token from Telegram | **verified** (`TestBridgeOffersLogin`, local run) | 2026-10-07 |
| The bot login first calls the Bot API `logOut` for the bridge bot and tolerates "Logged out" on later runs | **verified** (code) | `pkg/connector/loginbot.go` at `9b2a6e3e` |
| Bots never receive other bots' messages in groups; in channels, administrator bots receive all posts | **assumed** (Bot API behavior); the test refuses groups with an explicit message | — |
| The bridge, logged in as a bot, creates a portal for the channel on the first post and relays both ways | **unknown** until the first CI run with the tokens | — |
| The public test `api_id` is accepted on production for a bot login | **unknown** until that run (Telegram documents it as limited, for testing) | <https://core.telegram.org/api/obtaining_api_id> |

## Consequences

- The message test runs in CI only (the tokens are write-only GitHub secrets); local runs need the maintainer to export the two tokens.
- A bot login is not a user login: what is verified is the bridge pipeline (Telegram update → portal → Matrix, and back). User-specific behavior (contacts, private chats, QR or phone login) is covered later, in T1.6, against a real account on the maintainer's machine, never in CI.
- If the bot approach fails on Telegram's side, the fallback is option 1 (a dedicated number) or option 4.

## Sources

- mautrix-telegram, `pkg/connector/loginbot.go` and `docker-run.sh`, commit `9b2a6e3ea348b37ff0076db5473e08acb71b0767`.
- Telegram Bot API: <https://core.telegram.org/bots/api> (getUpdates, sendMessage, logOut).
- ADR 0005 for the test-environment findings.
