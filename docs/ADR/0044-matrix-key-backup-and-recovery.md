# ADR 0044 — Key backup and recovery: a Matrix account's recovery key verifies Musubee and opens its history

- **Status**: proposed (waiting for the maintainer's answers below; no code yet)
- **Date**: 2026-11-03
- **Task**: T2.28 (E2.8)
- **Deciders**: Claude Code (design assistant); the maintainer decides

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

## Proposed decision

**Option A now; Option B as a follow-up if the maintainer wants it; C later.**

- **API**: `accounts.verify` (`account_id`, `recovery_key`) → `{ verified, keys_imported }`; `Account.verification` (`unverified`, `verified`, `not_supported`) for Matrix accounts; errors `invalid_params` (malformed key), `unauthorized` (wrong key), `network`.
- **Core** (`core/connector/matrix`): the client gains `Verify(ctx, recoveryKey)`; uploads new inbound sessions in batches after each sync while a trusted backup version is known (stored as the machine's `KeyBackupVersion`); a sync that finds the backup's version changed stops uploading until the user verifies again.
- **Interface**: in the account's line, "Verify this device…" opens a dialog explaining what a recovery key is ("the key your Matrix app gave you when you set up secure backup"), a password-type field (never logged, never stored, cleared after use), and the outcome.
- **Logs**: never the recovery key, never secrets; counts only.
- **Tests**: the Synapse journey sets up recovery for alice with mautrix-go (`GenerateAndUploadCrossSigningKeys` with her password, a backup), has bob's messages encrypted before Musubee's login, logs Musubee in, checks they are undecryptable, verifies with the key, and checks that they decrypt, that bob's client sees the device as cross-signed, and that a message Musubee received is in the backup.

## Questions for the maintainer

1. **Scope**: Matrix accounts only, as above? (Telegram and Signal do not need it; see the table.)
2. **Option B**: should Musubee also set recovery up for accounts without it (showing a recovery key once, asking the account's password)? Or leave that to Element for now?
3. **Uploading to the backup**: on by default once the device is verified (proposed), or a separate switch?
4. **Where in the interface**: the account's line in the sidebar (proposed), or a settings screen per account?

## Consequences (if accepted as proposed)

- A Matrix user who reinstalls Musubee, or adds it next to Element, gets their history back with their recovery key, and is trusted by their contacts' clients.
- Without a recovery key, nothing changes: new devices still start without history.
- The recovery key passes through the interface and the core in memory only; a compromised device at that moment could read it, as with any Matrix client.

## Verification

Nothing is implemented; the claims above about mautrix-go are **verified by reading** mautrix-go v0.31.0 (`crypto/cross_sign_ssss.go`, `crypto/keybackup.go`, `crypto/ssss/`, `crypto/backup/`, `crypto/sql_store.go`, `client.go`), 2026-11-03.
