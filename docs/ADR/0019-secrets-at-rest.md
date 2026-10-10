# ADR 0019 — Secrets at rest: sessions sealed in the database, a master key in the OS secure storage

- **Status**: accepted
- **Date**: 2026-10-10
- **Task**: T2.3
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

Until T2.3 the core's SQLite database (`core.db` in the data directory) held every account's session in clear:

- **Telegram**: bridgev2 stores each login's metadata as JSON in `user_login.metadata`. mautrix-telegram's metadata holds the MTProto session, including `auth_key` (the 2048-bit authorization key that *is* the Telegram session: whoever has it is logged in as the user) and `push_encryption_key`.
- **Matrix** (T2.2, ADR 0018): the same column holds the account's `access_token`. The Olm account and the Olm/Megolm sessions are stored by mautrix-go's crypto store, encrypted ("pickled") with a pickle key; that key was a constant in the source code, so the encryption protected nothing.

Anyone who copies the data directory (a backup, another program running as the user, a stolen unencrypted disk, a debugging copy) could therefore take over the user's accounts. CLAUDE.md §6.8 requires keys in the OS secure storage.

Constraints:

- **Every platform**: Windows, macOS, Linux (Electron, the core as a child process), Android (the core in-process through JNI) and iOS later (E3.3). The solution must not block iOS, where the core is a static library and JIT is forbidden.
- **Pure Go where possible**, and the two SQLite drivers already in use: `modernc.org/sqlite` (pure Go, desktop) and `mattn/go-sqlite3` (cgo, Android, ADR 0011).
- **Unchanged upstream code**: bridgev2 and mautrix-telegram run unmodified (ADR 0009, ADR 0014); they write the metadata themselves, through `go.mau.fi/util/dbutil`.
- **Fail closed**: a value that cannot be sealed must not be written in clear, and a wrong key must not look like an empty account list.
- **No secret in logs** (CLAUDE.md §6.8), and an error must never quote a key or a session.

## Options considered

### Option A — Encrypt the whole database (SQLCipher, SQLite3 Multiple Ciphers)
- Pros: everything is protected, messages included; one key; well-known formats.
- Cons:
  - Both encryption extensions are C code. With `mattn/go-sqlite3` they need a different amalgamation (e.g. `mutecomm/go-sqlcipher`, `jgiannuzzi/go-sqlite3` with sqlite3mc), which means cgo on the desktop too, where the core program is built without cgo today (`modernc.org/sqlite`, ADR 0013).
  - With `modernc.org/sqlite` there is no maintained encryption: it is a transpilation of the official amalgamation, without codecs.
  - `ncruces/go-sqlite3` offers encrypting VFSes (Adiantum, XTS) in a pure-Go driver, but runs SQLite as WebAssembly through wazero, whose compiler needs executable memory; on iOS, JIT is forbidden, so it falls back to an interpreter that is much slower. Changing driver also means re-validating bridgev2, the crypto store and `localmatrix` on it, a project of its own.
  - SQLCipher's licence (BSD-style) is fine, but the build work lands on every target, Android and iOS included, before any user benefit.
- Verdict: the right long-term protection of message content, too large for T2.3.

### Option B — Seal the secrets only, in Go, with a master key in the OS secure storage
- Pros: pure Go (stdlib AES-GCM and HKDF); works with both drivers and on every target, iOS included; small, testable code; no change to upstream code if the sealing happens below it.
- Cons: message content, contact names and the rest of the database stay in clear; it relies on knowing which columns hold secrets.

### Where to seal (within option B)

#### B1 — Patch bridgev2 or mautrix-telegram
- Cons: a fork of fast-moving code (CLAUDE.md §6.5); every upgrade becomes a merge.

#### B2 — Wrap the connectors' metadata types
- Cons: the metadata type belongs to each connector (`UserLoginMetadata` in mautrix-telegram); wrapping it means intercepting its JSON marshalling for every network, and bridgev2 also copies metadata in places we would have to follow.

#### B3 — A wrapping `database/sql` driver
The core opens SQLite through a `driver.Connector` that wraps the real one. Writes to `user_login` have their `metadata` argument sealed; reads of any column named `metadata` whose value carries the sealed prefix are opened.
- Pros: no change upstream; one place for every network; both drivers; prepared statements and transactions included.
- Cons: it must recognise bridgev2's queries. A query it cannot follow (an expression on `metadata`, a positional `?`, several statements) is refused, so a mautrix upgrade that changes its queries fails loudly in tests instead of writing secrets in clear.

### Where to keep the master key

| Platform | Choice in T2.3 | Later |
|---|---|---|
| Windows | **DPAPI** (`CryptProtectData`, user scope, with an application entropy), in `core.key` in the data directory | Electron `safeStorage` could give it instead, if the core moves into a sandbox |
| Android | **Android Keystore**: an AES-256-GCM key that never leaves the Keystore wraps the master key, kept in `noBackupFilesDir`; the app gives the key to the core in its configuration (`database_key`) | StrongBox when available |
| macOS, Linux | a plain `core.key` file (mode 0600), with a warning in the log | Electron `safeStorage` (Keychain, libsecret/kwallet) gives the key to the core: follow-up task |
| iOS | not built yet | the Keychain, in E3.3 (the NSE needs the key too: shared Keychain access group) |

The Windows Credential Manager was the other option of the task. DPAPI is what it uses underneath, it has no size limit issue and needs no entry name, and a DPAPI blob in the data directory moves with it; both protect the same way (see the threat model).

## Decision

**Option B with B3**: the accounts' sessions are sealed with AES-256-GCM by a wrapping `database/sql` driver, under keys derived with HKDF-SHA256 from a 32-byte master key kept by the OS secure storage (DPAPI on Windows, the Android Keystore on Android; a plain file on macOS and Linux until follow-up tasks). The Matrix pickle key is derived from the same master key. Encrypting the whole database (option A) is deferred until message content needs protection beyond the OS disk encryption.

### Design

- **`core/secrets`**: `Key` (32 random bytes), `Derive(purpose)` = HKDF-SHA256 with the info `"musubee/v1/" + purpose`; `Sealer` = AES-256-GCM with a random 96-bit nonce and the purpose as additional data, so a value sealed for one purpose does not open for another. Format: `"musubee-sealed:v1:" + base64(nonce ‖ ciphertext ‖ tag)`. The prefix tells sealed values from plain ones (databases written before T2.3) and carries a version for later changes.
- **Purposes**: `user_login.metadata` (sessions), `matrix/pickle-key` (the crypto store's pickle key), `key-check`.
- **Key check**: a table `musubee_key_check` holds a constant sealed with the key. At start, a different key gives `ErrWrongKey` and the core does not open: a wrong key never looks like "no accounts", and nothing is overwritten.
- **Migration**: at start, plain `user_login.metadata` rows are sealed, then the write-ahead log is checkpointed, the file is rebuilt with `VACUUM` and the log is truncated again, so the old values do not stay in free pages or in the log.
- **Master key source**: the application gives it (`database_key` in the core's configuration, base64), or the core keeps its own `core.key` (`{"version":1,"protection":"dpapi"|"none","key":...}`), written atomically with mode 0600. A file protected with DPAPI is refused on a platform without DPAPI.
- **Android**: `CoreKey` generates 32 random bytes, wraps them with an AES-256-GCM key of the Android Keystore (alias `app.musubee.core.key-wrap`, not exportable, IV chosen by the Keystore, additional data naming the use) and stores the result in `noBackupFilesDir/core-key` with `AtomicFile`. `CoreService` passes the key to the core and clears its copies of the key and of the configuration after use. If the Keystore key is gone, loading fails: a new key could not open the database anyway.
- **Matrix logins made before T2.3** must sign in again: their crypto store was pickled with the old constant, which is no longer used. Matrix accounts were added just before, in T2.2, and have no users yet.

### Threat model

| Attacker | Protected? |
|---|---|
| A copy of the data directory alone (backup, sync tool, a disk read without the OS session, a file sent for debugging) | **Yes** on Windows and Android: the key is bound to the user's Windows account or to the device's Keystore. **No** on macOS and Linux until the follow-up task. |
| Another program running as the same user, on Windows | **No**: DPAPI decrypts for any process of the user; the entropy is in the source code. Android's app sandbox does protect against other apps. |
| Root / administrator, or malware with the user's rights while the core runs | **No**: it can read the core's memory. Out of scope for any client. |
| A copy of the Android app's files to another device | **Yes**: the Keystore key does not leave the device. |

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| bridgev2 stores each login's metadata as JSON in `user_login.metadata`, through the queries tested in `core/storage/sqlite/sealed_test.go` | **verified** (read in the code) | `maunium.net/go/mautrix@v0.31.0/bridgev2/database/userlogin.go`, 2026-10-10 |
| mautrix-telegram's login metadata holds `auth_key` and `push_encryption_key` | **verified** (read in the code) | `go.mau.fi/mautrix-telegram@v0.2609.0/pkg/connector/metadata.go`, lines 112 and 125, 2026-10-10 |
| dbutil rewrites `$N` placeholders into `?N` before the SQLite driver sees them, so the driver must accept both | **verified** (read in the code, and a test failed until both were accepted) | `go.mau.fi/util@v0.10.1/dbutil/database.go`, `replacePositionalParams`, 2026-10-10 |
| mautrix-go's crypto store encrypts the Olm account, the sessions and its stored secrets with `PickleKey` | **verified** (read in the code) | `maunium.net/go/mautrix@v0.31.0/crypto/sql_store.go` (`PutAccount`, `PutSecret`), 2026-10-10 |
| No session is readable in the database file after an echo login, a Matrix login (no `syt_` token prefix, no `"access_token"`) or a Telegram login (no `"auth_key"`), write-ahead log included | **verified** for the echo network (Windows, `core/embedded/secrets_test.go`) and the Android emulator (API 30, `CoreKeyTest`); Matrix and Telegram journeys are **verified on CI** only (Synapse needs Docker, the bot tokens are CI secrets) | `infra/matrixtest`, `infra/telegramtest/ondevice` |
| Plain sessions of an older database are sealed at start and no longer appear in `core.db` or `core.db-wal` | **verified** (test with a random marker) | `TestPlainSessionsAreSealedAtStart` |
| DPAPI protects the key file on Windows, and the blob does not open without the application entropy | **verified** (Windows 11, real DPAPI) | `core/secrets/protect_windows_test.go` |
| DPAPI does not protect against other programs running as the same user | **verified** (documented behaviour) | Microsoft Learn, "CryptProtectData function", 2026-10-10 |
| The Android Keystore key is not exportable (`getEncoded()` returns null) and the key file holds no copy of the master key | **verified** (emulator API 30) | `CoreKeyTest` |
| The Android Keystore key is in secure hardware on the maintainer's phone | **assumed** (Pixel 8 Pro has a TEE and StrongBox; not measured with `KeyInfo`) | — |
| Some other table of mautrix-telegram holds session material | **assumed not**: its other tables hold update state and access hashes, which identify peers but do not log in | read in `pkg/store`, 2026-10-10 |
| The query analysis survives the next mautrix upgrade | **unknown**: checked by `TestSealedArgument` and the journeys at each upgrade; an unsupported query fails closed | `mautrix-upgrade-pending` |
| macOS and Linux key storage through Electron `safeStorage` | **unknown**: follow-up task | — |
| iOS Keychain, and the key in the notification extension | **unknown**: E3.3 | ADR 0015 |

## Consequences

- No session key is readable in the database file on Windows and Android; the Matrix crypto store is now really encrypted.
- **Losing the key loses the sessions**: a restored data directory without its key (another Windows account, a reinstalled Android app) fails to open with "the database was written with another key". The user has to remove the data and sign in again; an interface for that comes with account management (E2.2).
- Message content, names and pictures stay in clear in the database; they rely on the OS disk encryption (BitLocker, FileVault, Android file-based encryption). Revisit option A when the threat model needs more, or if `ncruces/go-sqlite3` becomes usable on iOS.
- A mautrix upgrade that changes bridgev2's `user_login` queries makes the core fail on login until `sealedArgument` follows; the tests catch it first.
- Follow-up tasks: macOS and Linux keys through Electron `safeStorage`; iOS Keychain (E3.3); an interface to recover from a lost key (E2.2).

## Sources

- mautrix-go v0.31.0: `bridgev2/database/userlogin.go`, `crypto/sql_store.go` (module cache, 2026-10-10).
- go.mau.fi/util v0.10.1: `dbutil/database.go` (2026-10-10).
- mautrix-telegram v0.2609.0: `pkg/connector/metadata.go` (2026-10-10).
- Microsoft Learn, CryptProtectData function (dpapi.h): https://learn.microsoft.com/en-us/windows/win32/api/dpapi/nf-dpapi-cryptprotectdata (2026-10-10).
- Android Developers, Android Keystore system: https://developer.android.com/privacy-and-security/keystore (2026-10-10).
- Go standard library: `crypto/hkdf` (Go 1.24+), `crypto/cipher` GCM.
- SQLCipher: https://www.zetetic.net/sqlcipher/ ; SQLite3 Multiple Ciphers: https://utelle.github.io/SQLite3MultipleCiphers/ ; ncruces/go-sqlite3 encrypting VFSes: https://github.com/ncruces/go-sqlite3 (2026-10-10).
