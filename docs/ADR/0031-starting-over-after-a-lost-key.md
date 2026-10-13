# ADR 0031 — Starting over after a lost key: the shell sets the old data aside, never deletes it, and starts a new core

- **Status**: accepted
- **Date**: 2026-10-13
- **Task**: T2.15
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

The core's master key seals the accounts' sessions in its database (ADR 0019). The key lives in the system's secure storage: the Android Keystore wraps it (`CoreKey.kt`), DPAPI protects it on Windows (`core/secrets/keyfile.go`), and Electron's safeStorage keeps it on macOS and Linux (ADR 0027). Every one of them can lose it for good:

- the app's files are restored on another device or another user account, where the Keystore key, the DPAPI master key or the Keychain item does not exist;
- the Keystore is cleared (some devices do so when the screen lock is removed), a Windows password is reset by an administrator, or the keyring is reset;
- a key file is replaced by one written with another key.

Until T2.15 the core then failed closed, as ADR 0019 requires: it refused to start, since a new key would make every sealed session unreadable. But the app could do nothing more. The desktop app showed "Musubee could not start: …" and restarted the core up to five times; the Android app showed the same line. The user had no way back to a working app short of clearing the app's data by hand. ADR 0019 and ADR 0027 left "an interface to recover from a lost key" to E2.2.

## What counts as a lost key

Only failures that no retry can fix are a lost key. Each shell reports them in its own way:

| Where | Lost key | Not a lost key |
|---|---|---|
| Core (`core/secrets`) | `ErrWrongKey`: the database was written with another key. A DPAPI key file that DPAPI refuses (or any DPAPI file off Windows). Both match `secrets.ErrKeyLost` with `errors.Is`. | An invalid key file, or one with an unknown protection, which a later core may read. Any other error. |
| Core program | Exit status 3 (`exitKeyLost`) | Exit status 1 |
| Android (`CoreKey.kt`, JNI) | `KeyLostException`: the Keystore key is gone, the Keystore cannot decrypt the key file (`AEADBadTagException`), or the core refuses the key (`ErrKeyLost`, thrown by name from `core/ffi/jni_android.go`) | `IllegalStateException`: a key file in an unknown format |
| Desktop (`core-key.ts`) | `CoreKeyLostError`: safeStorage cannot decrypt the key file | `CoreKeyError`: the secure storage is unavailable, or the key file is invalid |

The shells turn this into the status `{ state: "failed", keyLost: true }` (`ui/src/shell.ts`). The desktop's supervisor does not restart a core that exits with status 3: no restart can help.

## Options considered

### Option A — Start over, setting the old data aside (chosen)
On a lost key, the interface explains what happened and offers "Start over…", with a confirmation. The shell renames the core's data directory (and, on Android, the key file kept outside it) to `<name>-unreadable-<UTC time>`, next to where it was, then starts a new core, which creates a new key.
- Pros: the user gets a working app in one step. Nothing is lost that was not lost already. If the key comes back (a keyring that was only locked, a backup of the Keystore or the key file), renaming the folder back restores everything.
- Cons: the old folder takes space until someone deletes it. It also keeps the message history, which the core does not seal (ADR 0019 seals the sessions only). The history is as readable there as it was in the live database, no more.

### Option B — Start over, deleting the old data
- Pros: no space taken, nothing left behind.
- Cons: the decision is irreversible, and it rests on a diagnosis that may be wrong. Linux's keyring is the known doubtful case (see Verification). One wrong click would erase a history that only this device held.

### Option C — Only explain, and let the user clear the app's data
- Pros: no code that touches the core's files.
- Cons: the user has to find the system's "Clear data" or the app's data folder. That is the jargon E2.2 removes, and clearing everything also deletes what option A keeps.

### Option D — A core API command that resets the core
- Pros: one implementation for every shell.
- Cons: the core is precisely what does not start. The reset must happen before a core runs, so it belongs to the shell, which owns the data directory and the key.

## Decision

**Option A**, in the shells:

- **Shell contract** (`ui/src/shell.ts`): the `failed` status gets `keyLost?: true`. `ShellCore` gets an optional `startOver()`, which resolves once the new core is starting. The status then goes through `starting` and `ready` again.
- **Desktop** (`apps/desktop`): the preload exposes `startOver()`, which invokes `core:startOver`. The main process accepts it only from a trusted window (the same check as `core:call`), takes no argument, and refuses unless the core's status is `failed` with `keyLost`. It then renames `<userData>/core` aside, forgets the failure (`CoreProcess.prepareRestart()`) and starts the core as at launch, which creates a new key in safeStorage on macOS and Linux, or with DPAPI on Windows.
- **Android** (`apps/mobile/android`): `CoreService.startOver()` refuses unless the core failed with `KeyLostException`. It renames `files/core` and `no_backup/core-key` aside, then opens a new core in the same service. The plugin rejects `call` with the code `KEY_LOST` and exposes `startOver()`.
- **Interface** (`ui/src/app/App.tsx`): on `keyLost`, the app shows "Musubee cannot open your data" instead of the inbox. It says what happened and its consequences in plain words: the accounts are added again; conversations come back from the networks that keep them; nothing is deleted. "Start over…" asks for a confirmation. The store forgets everything it showed before calling the shell.

## Consequences

- A core that cannot start for another reason (a damaged key file, an unavailable keyring, a full disk) still shows "Musubee could not start: …", without a way to start over. Those cases may resolve themselves; starting over would hide the cause.
- The folders set aside are never deleted by the app. Offering to delete them, with what they contain (the message history in clear, the sessions sealed with the lost key), is a follow-up.
- The Linux shell may report a lost key when safeStorage could not reach the keyring (see Verification). The user who starts over then has two folders, and renaming them back recovers the old one: this is why nothing is deleted.
- Logging: the shells log that they started over and the folder's name; never the key, which the new core logs as before (its protection only).
- The desktop's preload adds a fourth function to `window.musubee.core`; the isolation test lists it.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| `ErrWrongKey` and a DPAPI file that DPAPI refuses match `ErrKeyLost`; an invalid or unknown key file does not | **verified** (Windows) | `core/secrets/secrets_test.go` `TestLostKeys`; `core/embedded/secrets_test.go` |
| The core program exits with status 3 on another key | **verified** (Windows) | `core/cmd/musubee-core/main_test.go` |
| The desktop supervisor fails at once on status 3, without restarts, and starts a new core after `prepareRestart()` | **verified** (Windows, the real core) | `apps/desktop/test/core-process.test.ts` |
| safeStorage's decrypt failure is a lost key; an unavailable secure storage or an invalid file is not | **verified** with a stand-in for safeStorage | `apps/desktop/test/core-key.test.ts` |
| The desktop app shows the recovery screen, starts over on confirmation, keeps the old folder, and the new key opens again after a restart | **verified** on Windows (DPAPI); on macOS and Linux by CI (safeStorage) | `apps/desktop/e2e/app.spec.ts`, "a lost key offers to start over" |
| `core:startOver` is refused while the key opens | **verified** (Windows) | same journey |
| Android: a gone Keystore key, a Keystore key that cannot decrypt, and the core's `ErrKeyLost` through JNI are `KeyLostException`; an unknown key file is not | **verified** on a Pixel 8 Pro (Android 17) | `CoreKeyTest` |
| Android: the service starts over once the key is lost, sets aside both the data and the key file, and refuses with a working core | **verified** on a Pixel 8 Pro (Android 17) | `StartOverTest` |
| Android app: with a damaged key file, the app shows the recovery screen; "Start over" gives an empty app; an account added then survives a restart | **verified** by hand on a Pixel 8 Pro (Android 17), 2026-10-13 | `files/core-unreadable-20261013T192532Z` and `no_backup/core-key-unreadable-20261013T192532Z` on the device |
| The mobile shell maps `KEY_LOST` to `keyLost` and starts over | **verified** with a scripted plugin | `ui/src/capacitor.test.ts` |
| safeStorage on Linux fails to decrypt only when the key is truly lost, never when the keyring is locked or unreachable | **unknown**: Chromium may fall back to another password when the keyring does not answer; not reproduced | |
| A Windows password reset by an administrator makes DPAPI refuse the key | **assumed** from Microsoft's documentation of DPAPI master keys; not reproduced | |
