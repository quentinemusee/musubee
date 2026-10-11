# ADR 0026 — Signal's secrets at rest: signalmeow's store seals its secret columns, in our copy

- **Status**: accepted
- **Date**: 2026-10-11
- **Task**: T2.9
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

ADR 0019 seals the accounts' sessions in the core's database: bridgev2's `user_login.metadata` column, through a wrapping `database/sql` driver, with AES-256-GCM keys derived from a master key that the OS secure storage keeps. ADR 0025 found that Signal does not fit that design. mautrix-signal keeps its secrets in signalmeow's own tables of the same database, in clear (`pkg/signalmeow/store/upgrades/00-latest.sql`):

| Table | Secrets |
|---|---|
| `signalmeow_device` | the account (ACI) and phone number (PNI) identity key pairs, the account password, the master key, the account record (a protocol buffer that holds the profile key), the account entropy pool, the backup keys |
| `signalmeow_sessions` | the Signal protocol sessions (ratchet state) with each contact device |
| `signalmeow_pre_keys`, `signalmeow_kyber_pre_keys` | our one-time, signed and Kyber pre-keys, with their private keys |
| `signalmeow_sender_keys` | the group sender keys |
| `signalmeow_recipients.profile_key` | each contact's profile key, which decrypts their profile |
| `signalmeow_groups.master_key` | each group's master key, which decrypts its state |
| `signalmeow_event_buffer.plaintext` | the **plaintext** of messages decrypted but not yet handled |
| `signalmeow_backup_recipient`, `_chat`, `_message` | a restored backup's contacts, conversations and **messages** (protocol buffers), and the groups' master keys |

Whoever copies the data directory could therefore impersonate the user's Signal device and read the buffered messages. ADR 0025 blocked shipping Signal on this (decision 5), and `CLAUDE.md` §6.8 requires keys in the OS secure storage.

Since T2.6 the core builds mautrix-signal from a copy in our repository (`core/replace/mautrix-signal`, ADR 0025), with every change listed in its `MUSUBEE.md`.

Constraints:

- the master key and the sealing format of ADR 0019, so the user has one key;
- both SQLite drivers (`modernc.org/sqlite` on the desktop, `mattn/go-sqlite3` on Android);
- fail closed: a secret is never written in clear once sealing is on, and a value that does not open is an error, never data;
- a small, reviewable change to the copy, easy to carry over to the next mautrix-signal release;
- no secret in logs or errors.

## Options considered

### Option A — Extend ADR 0019's driver to signalmeow's columns
The driver recognises the queries that write to `user_login.metadata` and seals one argument.
- Pros: no change to the copy; one sealing place for the whole core.
- Cons:
  - nineteen columns in nine tables, written by about twenty queries, some of them upserts with `excluded.` copies, and read by others, including a join on a group master key (`GetBackupChatByGroupID`);
  - reading is the hard part: ADR 0019's driver opens any column named `metadata` that carries the sealed prefix, because that name is unique to bridgev2's table. signalmeow's column names (`data`, `record`, `key_pair`, `master_key`, `password`) are generic, so the driver would have to parse the `SELECT` lists to know which result column is which. That is a SQL parser in the read path of every query of the core;
  - the analysis must follow upstream's query text, which changes more often than its Go code.
- Verdict: possible for writes, fragile for reads. Rejected.

### Option B — Encrypt the whole database
- Pros: protects everything, including message content held by other parts of the core.
- Cons: the reasons ADR 0019 gave still hold (cgo on the desktop or a driver change, nothing that works well on iOS). Not specific to Signal.
- Verdict: still deferred.

### Option C — Seal in signalmeow's store, in our copy
Every query of signalmeow lives in its `store` package (verified: no other package of mautrix-signal mentions a `signalmeow_` table). Each secret column is sealed in the Go code just before the query, and opened in the code that scans the row.
- Pros:
  - the code knows which value is which: no SQL analysis;
  - the store gets the sealer from the connector, so the key handling stays in the core (`core/secrets`);
  - a scan destination that opens the value keeps each change to one line in most places;
  - tests can check every column on a real database with real libsignal objects.
- Cons:
  - a larger diff to our copy, about 120 lines over nine files plus one new file, to re-apply at each upgrade (`MUSUBEE.md` lists it);
  - a `SELECT` that a future upstream version adds on a secret column would read the sealed text and fail to deserialize. That fails closed, and the copy's test catches it, because it fills and reads every column.
- Verdict: chosen.

### Where the sealer lives (within option C)
- **Process-wide** (`store.SetSealer`, like `signalmeow.SetEnvironment`): the smallest diff, since the store's row scanners are package-level functions. But two cores in one process with different keys (the core's tests do this) would share one key, and a refused second key would break them. Rejected.
- **Per store** (`Container.SetSealer`): each database has its own sealer. The scanners that read a sealed column become methods of `Container` to reach it. Chosen.

## Decision

**Option C, per store.** signalmeow's store, in our copy, seals its nineteen secret columns with a `Sealer` that the core gives it; the core's sealer is AES-256-GCM under keys derived from ADR 0019's master key, one key per column.

### Design

- **In the copy** (`pkg/signalmeow/store/sealing.go`, change 2 of `MUSUBEE.md`):
  - `Sealer` has `Seal(purpose, plain) []byte` and `Open(purpose, sealed) ([]byte, error)`. The purpose is the column, `"table.column"` (`store.Purposes` lists the nineteen). Sealed output must be valid text, since `password`, `account_entropy_pool` and the group master keys are TEXT columns.
  - `Container.SetSealer` is called once, before the store is used. Without a sealer the store behaves as upstream's, and the copy's tests check that too.
  - NULL stays NULL: the event buffer marks a handled event by clearing its plaintext, and deletes old events with `plaintext IS NULL`.
  - With a sealer, a stored value that does not open (in clear, sealed with another key, or for another column) is an error naming the column, never the value.
  - `GetBackupChatByGroupID` joined `signalmeow_groups.master_key` to `signalmeow_backup_recipient.group_master_key` in SQL. Sealed values have random nonces, so with a sealer the store reads the backup's group recipients and compares the opened keys in Go. A restored backup has one row per group, so the cost is small.
- **In the core** (`core/connector/signal`):
  - `signal.Config.Key` (the master key) is required: without it, `New` returns `ErrNoKey`, so a Signal store in clear cannot happen by omission. `core/embedded` passes the same key as ADR 0019's.
  - `New` returns mautrix-signal's connector wrapped in `sealedConnector`, whose `Init` gives the store its sealer right after upstream's `Init` creates the store and before `Start` opens it. The wrapper embeds the connector's pointer, so bridgev2 still finds every optional interface.
  - The sealer derives one `secrets.Sealer` per column, with the purpose `"signal/" + column` (HKDF info `"musubee/v1/sealer/signal/signalmeow_sessions.record"`, for example). Values have ADR 0019's format, `"musubee-sealed:v1:"` and base64, also stored as text in BLOB columns. An empty value opens as empty, never as NULL.
- **No migration.** No Musubee build has shipped Signal (ADR 0025), so no database holds signalmeow rows in clear. Such a row fails to open, and the user links the device again.

### What stays in clear

| Data | Why |
|---|---|
| Phone numbers, contact and profile names, nicknames, avatar paths, `about` texts (`signalmeow_recipients`); the account's phone number (`signalmeow_device.number`) | personal data, not keys. It is the same class as the names and message content that the rest of the core keeps in clear (ADR 0019, Consequences), and it relies on the OS disk encryption |
| Contacts' identity keys and trust (`signalmeow_identity_keys`) | public keys. Changing them would change safety numbers, not reveal a secret |
| Sender key distribution IDs and group identifiers (`signalmeow_outbound_sender_key_info`, `signalmeow_groups.group_identifier`) | identifiers, which cannot decrypt anything |
| Registration IDs, device IDs, ACI and PNI UUIDs | identifiers |
| `signalmeow_profile_keys` | a legacy table. No query of v0.2609.0 reads or writes it; the profile keys moved to `signalmeow_recipients` |

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| Every query of signalmeow is in `pkg/signalmeow/store`; no other package of mautrix-signal mentions a `signalmeow_` table | **verified** (grep of the copy) | `core/replace/mautrix-signal`, 2026-10-11 |
| The nineteen columns above hold the secrets; the columns left in clear hold no key | **verified** (read in the schema and the store's queries) | `pkg/signalmeow/store/upgrades/00-latest.sql`, `pkg/signalmeow/store/*.go` |
| With a sealer, every one of the nineteen columns holds only sealed values, and each reads back as written (device, session from a real pre-key bundle, pre-keys, Kyber pre-key, sender key, profile key, recipient, group master key, event buffer, backup with the group lookup) | **verified** (Windows, mattn/go-sqlite3, real libsignal 0.102.2) | `pkg/signalmeow/store/sealing_test.go`, `TestSealedColumns` |
| The test catches a column written in clear | **verified** (the session's sealing removed by hand: the test failed, then restored) | — |
| Without a sealer, the store writes upstream's values | **verified** | `TestSealedColumnsWithoutSealer` |
| A value in clear, or sealed for another column, is refused with the column's name | **verified** | `TestSealedColumnRefusesUnsealedValue` |
| A cleared event plaintext stays NULL, and old events are still deleted | **verified** | `TestClearedEventPlaintextStaysNull` |
| On the core's own driver (modernc.org/sqlite) with the real sealer, the device's columns are sealed, and another key does not open them | **verified** (Windows) | `core/connector/signal/sealer_test.go` |
| After a real link to Signal's staging service and a Note to Self each way, every sealed column of `core.db` holds only sealed values | **verified** (Windows, 2026-10-11, the maintainer's staging account): the device's columns but the ephemeral backup key (NULL), 2 sessions, 201 pre-keys, 199 Kyber pre-keys and 1 profile key, all sealed; the other columns were empty (no group, no backup, the event buffer cleared). Not on CI yet (ADR 0025) | `infra/signaltest`, `checkSignalStoreSealed` |
| A future mautrix-signal release adds a query on a secret column | **unknown**: re-applying change 2 includes a grep of the new queries; an unsealed write fails `TestSealedColumns`, an unsealed read fails to deserialize | `MUSUBEE.md`, Updating |

## Consequences

- The Signal device's keys, the protocol sessions, the profile and group keys, and the buffered and restored message content are no longer readable in `core.db` on Windows and Android, under ADR 0019's threat model (macOS and Linux keep a plain key file until the `safeStorage` follow-up).
- This lifts the first of ADR 0025's two blockers for shipping Signal. The second, the sender certificate outcome, still holds, as do libsignal for Android on CI and the connector in the apps.
- Losing the master key loses the Signal device too, as for the other accounts (ADR 0019): the user links again.
- Each mautrix-signal upgrade re-applies change 2, which `MUSUBEE.md` describes, and runs the copy's sealing test.
- The cost: one AES-GCM operation per secret read or written, a derived key per column (cached), and about 40 bytes plus a third (base64) per stored secret. The test suite shows no visible slowdown; not measured on a phone.

## Sources

- mautrix-signal v0.2609.0, our copy: `pkg/signalmeow/store/*.go`, `pkg/signalmeow/store/upgrades/00-latest.sql`, `pkg/connector/connector.go` (`Init`, `Start`), 2026-10-11.
- maunium.net/go/mautrix v0.31.0: `bridgev2/bridge.go` and `bridgev2/matrix/*.go`, where bridgev2 checks the connector's optional interfaces with type assertions (2026-10-11).
- go.mau.fi/util v0.10.1: `dbutil/database.go`, `dbutil/iter.go` (2026-10-11).
- ADR 0019 (secrets at rest), ADR 0025 (Signal on the device).
