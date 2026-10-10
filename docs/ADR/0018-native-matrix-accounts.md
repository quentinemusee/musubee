# ADR 0018 — Native Matrix accounts: the core as an encrypted Matrix client, behind a bridgev2 connector

- **Status**: accepted
- **Date**: 2026-10-10
- **Task**: T2.2
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

T2.2 adds the user's own Matrix account (matrix.org, a company server, a self-hosted one) as a network of the core, next to Telegram. It is also the first half of the hosted-bridge mode (`CLAUDE.md` §2): with a hosted bridge, the core is a Matrix client of the homeserver where the bridge puts its portals, so the client built here is the one that mode will use.

Constraints:

- **No Matrix type in the API** (`CLAUDE.md` §2). A Matrix account is shown like any other network: accounts, conversations, messages, persons. Room IDs, user IDs (MXIDs) and event IDs must not reach the interface, which the tests check on every response and event (`apitest.MatrixIDs`, T2.1).
- **End-to-end encryption on the device** (`CLAUDE.md` §1). Most Matrix direct conversations are encrypted; a client without encryption is useless for them.
- **Pure Go where possible** (`CLAUDE.md` §2: "pure-Go Olm encryption (`goolm` build tag)"). libolm needs a C toolchain and headers on every target, including Android and iOS, and is deprecated upstream.
- **One storage**: the core's SQLite database already holds bridgev2's tables, the local homeserver (`localmatrix`, ADR 0009) and the persons (ADR 0017).
- **No message content in logs** (`CLAUDE.md` §6.8).
- **Tests against a real Synapse** (`CLAUDE.md` §6.2, ADR 0004), never a mock.

## Options considered

### How a Matrix account enters the core

#### Option A: a second kind of account, beside bridgev2
The core would sync the homeserver itself and write conversations and messages to its own tables, with a code path of its own in the API.
- Pros: no bridgev2 indirection; Matrix features (threads, reactions, read receipts) map one to one.
- Cons: two models of "conversation" in the core, two code paths for every API command, and the persons (ADR 0017), the event stream and the local homeserver would all need a second implementation. Every later feature would be built twice.

#### Option B: a bridgev2 network connector named `matrix`
The connector is a Matrix client of the user's homeserver; each joined room becomes a portal in the local homeserver, exactly like a Telegram chat.
- Pros: one model. Accounts, conversations, messages, the event stream, the leak check and the persons work unchanged; the API gains nothing Matrix-specific. bridgev2 already serializes the events of each portal, deduplicates messages and handles login, logout and restarts.
- Cons: a Matrix room is mirrored into another (local) Matrix room, which looks odd; features that bridgev2's `NetworkAPI` does not model must wait for its interfaces (edits, reactions and read receipts all have optional interfaces, not implemented yet).

### Encryption

#### Option C: mautrix-go's `cryptohelper` with goolm
- Pros: the reference Go implementation, used by mautrix's bridges and by gomuks; Olm and Megolm in pure Go with the `goolm` build tag; an SQL crypto store that fits the core's database.
- Cons: the build tag becomes mandatory everywhere: without it, `crypto/registerlibolm.go` (`//go:build !goolm`) links libolm through cgo.

#### Option D: no encryption in T2.2
- Pros: smaller task.
- Cons: fails the acceptance criterion (an encrypted conversation) and most real direct conversations.

### Where the client's stores live

#### Option E: the core's database, shared tables keyed per account
`crypto.NewSQLCryptoStore` on the core's database, one account ID per login and device; one `sqlstatestore` for every login.
- Pros: one file, one backup, one transaction model, the same upgrade mechanism as the other tables.
- Cons: the tables are shared by every Matrix login, so logout must delete its rows explicitly; the room state store is not per account (see Consequences).

#### Option F: one SQLite file per account
- Pros: logout deletes a file.
- Cons: several files to protect at rest (T2.3), to back up and to sync later; a second database handle per account on mobile.

## Decision

**Option B, with C and E**: a bridgev2 connector `matrix` (`core/connector/matrix`) in which the core is an end-to-end encrypted Matrix client of the user's homeserver (mautrix-go's `cryptohelper` on goolm), with its crypto and state stores in the core's database. Every Go build of the core uses the `goolm` build tag.

### Design

- **Identifiers.** Login ID = the user's MXID; portal key = `(room ID, login ID)`, so two accounts in the same room have two conversations; ghost ID = the member's MXID. These stay inside the core: the API shows its own opaque IDs (ADR 0012).
- **Sign-in.** A password flow with three fields (server, user name, password). The server is discovered through `.well-known` (`mautrix.DiscoverClientAPI`), or taken as typed when it contains `://`, else `https://` + the name. A wrong password becomes "wrong username or password"; the core's errors never quote what the user typed. The device gets a display name from the configuration.
- **Sessions.** Signing in again with the same MXID ends the previous device's session first (bridgev2 reuses the login with the same ID). Logout calls `LogoutRemote`, which bridgev2 calls *instead of* `Disconnect`: the client therefore disconnects, logs the device out on the homeserver (which deletes the device and its keys there) and deletes the device's rows from the crypto tables.
- **Crypto store.** One `SQLCryptoStore` per login, account ID `MXID/deviceID`: a new device after a new sign-in never mixes with the old one's keys. The store also keeps the sync token. The pickle key is a constant of the code until T2.3.
- **State store.** One `sqlstatestore` shared by the logins; since it is not managed by `cryptohelper`, the client registers `StateStoreSyncHandler` itself (`cryptohelper.Init` only does it for a managed store).
- **Sync.** Timeline limit 20. Before the events of each sync response, rooms without a portal, or whose name or members changed, are announced (`ChatResync` with portal creation); left rooms are deleted for this login only. The login is *connecting* until the first sync, *connected* after it; an unknown token is *bad credentials* (the user signs in again); other failures are *transient* with a retry after 10 s.
- **Direct conversations.** A room is direct if it is listed in the account's `m.direct` (read at start, then followed through account data). Invitations to direct rooms are accepted and recorded in `m.direct`, as Matrix clients do when the user accepts; invitations to groups are left pending.
- **Names.** A room's name is its name, or the names of its other members (up to three, then "and N others"). Ghosts get their member's display name; ghosts shared by several rooms are updated one at a time under a lock (the race found in ADR 0009).
- **Messages.** Text, notices and emotes, with replies (the reply fallback is stripped). Media keep their type and caption only. Edits are ignored. A message that cannot be decrypted becomes the notice "This message could not be decrypted on this device." Sent messages are encrypted when the room is, and the remote event ID becomes the message ID: the sync brings the message back, bridgev2 sees a duplicate and ignores it.
- **Logs.** mautrix logs the body of a failed request. Messages are therefore sent with `MakeFullRequest` and `SensitiveContent: true`, which replaces the body in the log (mautrix already does so for login requests with a password).

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| Without the `goolm` tag, `crypto` links libolm through cgo; with it, Olm and Megolm are pure Go | **verified** (read, and the build fails without the tag: `olm/olm.h` not found) | mautrix-go v0.31.0 `crypto/registerlibolm.go`, `crypto/registergoolm.go`, 2026-10-10 |
| `cryptohelper.Init` registers `StateStoreSyncHandler` only for a managed state store | **verified** (read) | `crypto/cryptohelper/cryptohelper.go`, `Init`, 2026-10-10 |
| `UserLogin.Delete` with `LogoutRemote` calls `LogoutRemote` and not `Disconnect` | **verified** (read) | `bridgev2/userlogin.go`, `Delete`, 2026-10-10 |
| `NewLogin` reuses an existing login with the same ID | **verified** (read) | `bridgev2/userlogin.go`, `NewLogin`, 2026-10-10 |
| bridgev2 ignores a remote message whose ID already exists in the portal | **verified** (read) | `bridgev2/portal.go`, "Ignoring duplicate message", 2026-10-10 |
| mautrix logs failed request bodies unless `SensitiveContent` is set (or `MAUTRIX_LOG_SENSITIVE_CONTENT=yes`) | **verified** (read) | `client.go`, `FullRequest.SensitiveContent`, 2026-10-10 |
| Sign-in, an encrypted direct conversation in both directions, a restart, a pending group invitation, logout with key purge, and no message or password in the core's log, against Synapse | **verified on CI only** (`infra/matrixtest`, job "Integration tests (Synapse + PostgreSQL)", Linux); not run on the maintainer's machine, whose Docker Desktop does not start | PR for T2.2 |
| The connector's pure functions (names, URLs, reply stripping, account IDs) | **verified** (unit tests, Windows) | `core/connector/matrix/names_test.go` |
| The core builds and its tests pass with goolm, with the race detector and with the cgo SQLite driver of Android | **verified** (Windows, Go 1.27.1, MSYS2 UCRT64) | `go test -tags=goolm -race ./core/...`, `go test -tags=goolm,musubee_cgo_sqlite ./core/...` |
| The connector works on Android and in the desktop app as it does in the Go tests | **assumed** (same core, same API; no device or Electron journey for Matrix yet) | T2.5 adds interface journeys |
| goolm's memory and CPU cost on mobile, and in the iOS notification extension | **unknown** | ADR 0015 measured the core without Matrix; to measure again in E3.3 |
| Behaviour with large accounts (hundreds of rooms, long histories) | **unknown** (the timeline limit is 20; no backfill) | E2.3 (pagination) |
| Homeservers other than Synapse (Conduit, Dendrite) | **unknown** | Manual tests before the public launch |

## Consequences

- **The goolm tag is part of the build.** CI (vet, lint, tests, race, shared library, iOS probe), the Telegram workflow, Gradle, the desktop build and its test setup, the transport benchmark, the tests that build the core, and the license audit all pass it. A build without it fails at compile time (`olm/olm.h`), so it cannot be forgotten silently. `CLAUDE.md` §9 shows it in every Go command.
- **Secrets are not protected yet.** The pickle key is a constant of the code and the access token is stored in plaintext in the login's metadata. Anyone with the database file can read the account's keys and use its session. T2.3 (secrets at rest) must cover both.
- **Known limits, to lift in later tasks:**
  - group invitations are not accepted (the user accepts them in another client); the core needs an "invitation" notion in the API first (E2.3);
  - edits, reactions, redactions and read receipts are not mirrored (E2.4);
  - media are placeholders: type and caption, no content (E2.4);
  - keys that arrive after a message (a late room key) do not decrypt it afterwards: the notice stays (E2.8, with key backup);
  - no key backup, no cross-signing, no device verification: other clients see the core's device as unverified (E2.8);
  - a member renamed in one room renames the ghost everywhere, and the last rename wins;
  - the state store is shared by every Matrix login of the core: two accounts in the same room share its member list, which is harmless (state is the same for both) but means a logout does not delete it;
  - no history backfill beyond the sync's timeline (E2.3).
- **Hosted bridges (ADR 0017).** A Matrix room is keyed `(matrix, room ID, MXID)`. A portal of a hosted bridge reached through this client will therefore look like a plain Matrix room, and a person link to it will name the Matrix room, not the remote chat. Deriving the network identity from the room's `m.bridge` / `uk.half-shot.bridge` state, as ADR 0017 requires, is future work of the hosted-bridge mode.
- **Logs.** No message text is logged by the connector; the integration test checks the core's log for the texts and the password. Any new request carrying user content must set `SensitiveContent`.

Revisit if bridgev2's model gets in the way of Matrix-specific features (threads, spaces) badly enough that a direct Matrix path (Option A) becomes simpler, or if goolm's memory cost rules it out in the iOS extension.

## Sources

- mautrix-go v0.31.0, read in the Go module cache, 2026-10-10: `crypto/registerlibolm.go`, `crypto/registergoolm.go`, `crypto/sql_store.go`, `crypto/cryptohelper/cryptohelper.go`, `sqlstatestore/`, `client.go` (`FullRequest`, `DiscoverClientAPI`, `Login`), `bridgev2/userlogin.go` (`NewLogin`, `Delete`), `bridgev2/portal.go` (duplicate messages), `bridgev2/ghost.go` (`UpdateInfo`).
- Matrix Client-Server API, `m.direct` account data and the reply fallback: https://spec.matrix.org/latest/client-server-api/, accessed 2026-10-10.
- ADR 0004 (Synapse test environment), ADR 0009 (bridgev2 in process, the ghost race), ADR 0012 (API contract), ADR 0015 (iOS memory), ADR 0017 (persons).
