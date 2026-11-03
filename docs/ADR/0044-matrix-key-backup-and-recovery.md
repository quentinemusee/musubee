# ADR 0044 — Key backup and recovery: a Matrix account's recovery key verifies Musubee and opens its history

- **Status**: accepted (the maintainer asked the design assistant to take the decisions, 2026-11-03)
- **Date**: 2026-11-03
- **Task**: T2.28 (E2.8)
- **Deciders**: Claude Code (design assistant), at the maintainer's request; to be reviewed by the maintainer

## Context

E2.8 asks for "key backup and recovery". What can be lost, per kind of key:

| Key | Where | If lost | Recovery |
|---|---|---|---|
| The core's master key (ADR 0019) | the OS secure storage | the device's sealed data | start over (ADR 0031): log in again; nothing on the networks is lost |
| Telegram's session | sealed in `core.db` | this device's login | log in again; Telegram keeps the history on its servers (cloud chats) |
| Signal's linked-device keys | sealed in `core.db` (ADR 0026) | this device's link | link again; a linked device never gets the history before its link (Signal's design), backup or not |
| **A Matrix account's end-to-end keys** (Olm account, Megolm sessions, ADR 0018) | the crypto store in `core.db`, pickled with a key derived from the master key | **the encrypted history**: a new Musubee device cannot decrypt messages from before it, and other people's clients show Musubee's device as unverified | **only through Matrix's key backup and secret storage**, which this ADR proposes |

So "key backup and recovery" is, for now, a Matrix question. The other networks keep their history on their servers, or by design do not give it to new devices.

Matrix's answer (client-server specification, "Secrets", "Server-side key backups", "Cross-signing"): the user's **recovery key** (or passphrase) opens their secret storage (SSSS) on the homeserver, which holds their cross-signing keys and the key of their server-side key backup. With it, a new device signs itself (other clients then trust it) and downloads the backup of room keys (the history decrypts).

What mautrix-go v0.31.0 gives (read in the module cache):
- `crypto.OlmMachine.VerifyWithRecoveryKey(ctx, recoveryKey)`: opens the default SSSS key with the recovery key, fetches the cross-signing keys, signs the own device and master key (`crypto/cross_sign_ssss.go`);
- `crypto/ssss`: `Machine.GetDecryptedAccountData` reads a secret (here `m.megolm_backup.v1`, `event.AccountDataMegolmBackupKey`);
- `OlmMachine.DownloadAndStoreLatestKeyBackup(ctx, backupKey)`: checks that the latest backup's `auth_data` matches the key (as the specification requires before trusting it), downloads and imports the room keys (`crypto/keybackup.go`);
- `GenerateAndUploadCrossSigningKeys(ctx, uiaCallback, passphrase)`: creates SSSS and cross-signing for an account without them (needs the account's password: user-interactive authentication);
- `backup.EncryptSessionData` and `Client.PutKeysInBackup`: the pieces to **upload** new room keys to the backup. The `OlmMachine` does **not** upload by itself (no call to `PutKeysInBackup` in `crypto/`): Musubee would add it.
- Secrets that the machine stores (`SQLCryptoStore.PutSecret`) are pickled with the store's pickle key, which the core derives from its master key (ADR 0019): at rest, as protected as the rest of the crypto store.

## Options considered

### Option A — Use the account's existing recovery: verify with the recovery key, download the backup, upload new keys (proposed)
1. In the Matrix account's settings, "Verify with your recovery key": the user types (or pastes) the key; the core calls `VerifyWithRecoveryKey`, then reads the backup key from SSSS, downloads the backup, and decrypts again the messages that failed ("Unable to decrypt" notices, T2.2).
2. While a trusted backup exists, the core uploads each new inbound Megolm session (its own and others') to it, encrypted to the backup's public key, so that the user's other clients read Musubee's messages after a reinstall.
3. The recovery key is never stored: only what it opens (cross-signing private keys, backup key), in the pickled crypto store.

### Option B — Option A, and set recovery up for accounts without it
For an account with no SSSS (often a fresh one): generate SSSS, cross-signing and a backup, ask the account's password (user-interactive authentication), and show the recovery key once, with a warning to keep it.
- Pros: a Musubee-only user gets backups too.
- Cons: the recovery key's whole life is Musubee's responsibility (shown once; lost means lost); password entry in the app.

### Option C — Interactive verification (emoji comparison) with another device
mautrix-go has `crypto/verificationhelper`.
- Cons: a whole flow of its own, with another device at hand; later, after A.

### Option D — Musubee's own backup of its whole database
- Cons: duplicates Matrix's mechanism for Matrix, and is useless for the networks that keep or refuse history (see the table); a separate decision with sync (ADR 0017).

## Decision

**Option A.** The questions asked of the maintainer, answered at their request:

1. **Scope**: Matrix accounts only (see the table above).
2. **Setting recovery up** (Option B): not now. Musubee uses an account's existing recovery; an account without secure backup is told to set it up in another Matrix app (Element). Showing a recovery key once and asking for the account's password are left for a later decision.
3. **Uploading to the backup**: on by default once the device is verified with the recovery key: the backup is the user's, and they asked for it in their other app.
4. **Where**: on the account's line in the sidebar, with the account's other actions.

- **API 1.15**: `accounts.verify` (`account_id`, `recovery_key`) → `backup_restored`; `Account.verification` (`verified`, `unverified`), omitted for other networks and while unknown. A wrong or malformed key is `invalid_params`, an account without secure backup or of another network `unsupported`, any other failure `network` (its message names no key).
- **Matrix connector** (`core/connector/matrix/recovery.go`):
  - opens the default secret storage key with the recovery key (`VerifyRecoveryKey`), fetches the cross-signing keys, signs the device and the master key (as `VerifyWithRecoveryKey` does, with the key opened once);
  - reads the backup key (`m.megolm_backup.v1`) from secret storage, keeps it with `PutSecret` (pickled with the key derived from the core's master key), restores the latest backup (`DownloadAndStoreLatestKeyBackup`, which first checks that the backup's public key is the key's), and remembers its version (`SetKeyBackupVersion`);
  - after each sync, uploads in batches of 100 the room keys that this backup version lacks (`GetGroupSessionsWithoutKeyBackupVersion`), each exported from its first known index and encrypted to the backup's public key (`backup.EncryptSessionData`), then marks them (`SetGroupSessionKeyBackupVersion`); a newer backup version (`M_WRONG_ROOM_KEYS_VERSION`) stops the uploads until the user verifies again;
  - remembers whether the device is verified (read once the encryption starts, and set by a verification), so that listing accounts makes no request.
- **Interface**: "Verify this device…" on an unverified Matrix account's line; a dialog with a password-type field (not remembered, not spell-checked, cleared once used); the outcome says whether the backup came back; "Verified device" afterwards.
- **Logs**: never the recovery key nor a secret; counts and errors only.

## Consequences

- A Matrix user who reinstalls Musubee, or adds it next to Element, gets their history back with their recovery key, and is trusted by their contacts' clients.
- Without a recovery key, nothing changes: new devices still start without history.
- The recovery key passes through the interface and the core in memory only; a compromised device at that moment could read it, as with any Matrix client.

- Messages that failed to decrypt before the verification stay as "Unable to decrypt" notices: the core replaced them when they came (T2.2), and the Matrix connector has no history fetching yet (ADR 0041) to bring them back. New messages, and history fetched later, decrypt with the restored keys.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| What mautrix-go v0.31.0 offers and lacks, as described above | **verified by reading** (`crypto/cross_sign_ssss.go`, `crypto/keybackup.go`, `crypto/ssss/`, `crypto/backup/`, `crypto/sql_store.go`, `client.go`), 2026-11-03 | |
| A wrong key is refused; the right one verifies the device (its cross-signature on the server), restores the backup, and a room key received afterwards reaches the backup; the key is in neither the log nor the database | **implemented, unverified** (CI only: Synapse) | `infra/matrixtest/recovery_integration_test.go` `TestMatrixRecoveryKey` |
| Other networks have no verification, and refuse it | **implemented, unverified** | `core/embedded/recovery_test.go` |
| The contract: the new command and fields have examples, valid for Go's and TypeScript's validators | **implemented, unverified** | `core/api` `TestExamplesCoverTheSchema`; `ui/src/core-api/contract.test.ts` |
| The store verifies and marks the account verified | **implemented, unverified** | `ui/src/store.test.ts` |
