# ADR 0027 — The core's master key on macOS and Linux: kept by the desktop app with Electron safeStorage

- **Status**: accepted
- **Date**: 2026-10-11
- **Task**: T2.10
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

ADR 0019 seals the core's secrets at rest (the accounts' sessions, the Matrix pickle key, and since ADR 0026 Signal's store) under a 32-byte master key that the OS secure storage should keep. Two platforms do so: DPAPI on Windows, where the core keeps its own `core.key`, and the Android Keystore, where the app gives the key to the core (`database_key`). macOS and Linux were left with a plain `core.key` (mode 0600) and a warning in the log, until this follow-up task. On those systems, a copy of the data directory alone (a backup, a sync tool, a file sent for debugging) holds both the sealed secrets and their key.

On the desktop, the core is a child process of the Electron app, spoken to on its standard input and output (ADR 0013). Electron's `safeStorage` encrypts strings with a key that the system keeps: the Keychain on macOS, and on Linux a password store (GNOME's libsecret, KWallet).

Constraints:

- the same master key and sealing as ADR 0019: this task changes where the key is kept, not how secrets are sealed;
- the key must reach the core without showing in the list of processes, in the environment, or in a log;
- fail closed: a key that the secure storage keeps but cannot give back must stop the core. A new key would make every sealed secret unreadable;
- honest about Linux desktops without a password store;
- existing data directories, whose `core.key` is plain, keep their accounts.

## Options considered

### Where the key is kept

#### A — The core reaches the system's secure storage itself
Use the Keychain (Security framework, through cgo) and the Secret Service (D-Bus) from Go.
- Pros: the core's own key file; no change to the app.
- Cons: cgo on macOS for the core program, built without cgo today (ADR 0013); a D-Bus client and a Secret Service implementation in Go to choose, license and maintain; KWallet as another case. Electron already does all of this, maintained by Chromium.
- Verdict: rejected.

#### B — The app keeps the key with `safeStorage` and gives it to the core
The same split as Android: the app holds the key, the core receives it.
- Pros: the platform's secure storage, through code that Chromium maintains for its own passwords; the core does not change, except for how it receives the key.
- Cons: the core program alone, without the app, cannot open such a data directory. That is intended: the key must not be readable without the user's session.
- Verdict: chosen, for macOS and Linux.

Windows keeps ADR 0019's DPAPI file. `safeStorage` uses DPAPI there too, so it would bring no protection, only a migration.

### `safeStorage`'s synchronous or asynchronous API
Electron's documentation recommends the asynchronous API (`encryptStringAsync`, `decryptStringAsync`). It does not block, it reports a rotated key (`shouldReEncrypt`), and on Linux it can use the Secret portal of Flatpak. The synchronous API "may be deprecated in a future version" and may block to ask the user for access on macOS and Linux.
- But on Linux, the documentation says how to detect a system without a password store only for the synchronous API: `getSelectedStorageBackend()` returns `basic_text`, and the data is then "encrypted via hardcoded plaintext password". For the asynchronous API it mentions a fallback provider "for environments without a secret service", with no way to detect it. Musubee must not tell the user the key is in the secure storage when it is encrypted with a password published in Chromium's source.
- Verdict: the **synchronous API**, used once at start-up, before any window shows. Revisit when the synchronous API is deprecated or the asynchronous one exposes its provider. The key file's format allows a new protection name then.

### How the key reaches the core
- **A command-line flag**: rejected, visible in the list of processes.
- **An environment variable** (like the Telegram credentials): rejected for a key. On Linux the environment is readable by other processes of the user (`/proc/<pid>/environ`); everywhere, child processes inherit it and crash reports may include it.
- **The first line of the standard input**, before the API stream (`musubee-core -key-from-stdin`): chosen. The pipe is private to the app and the core, and the supervisor writes the key again at every restart.

## Decision

**The desktop app keeps the core's master key with Electron `safeStorage` (synchronous API) on macOS and Linux, in the core's key file, and gives it to the core on the first line of its standard input.**

- **Key file**: `core.key` in the core's data directory, ADR 0019's format, with a new protection: `{"version":1,"protection":"electron-safestorage","key":<base64 of safeStorage.encryptString(base64 key)>}`. It is written through a temporary file renamed over the old one, with mode 0600, in a directory of mode 0700. It stays in the data directory, so a copy of the directory carries the encrypted key, which opens only through the user's Keychain or password store.
- **When safeStorage is usable**: on macOS, when `isEncryptionAvailable()` is true; on Linux, also when `getSelectedStorageBackend()` is not `basic_text` (nor `unknown`, before the app is ready).
- **Start-up** (`apps/desktop/src/core-key.ts`, `main.ts`):
  - Windows: nothing changes; the core keeps its DPAPI file.
  - A key file of `safeStorage`: decrypted and given to the core. If `safeStorage` is not usable or cannot decrypt it, the core does not start, and the interface shows the failed status with the reason. The file stays as it is.
  - No key file and `safeStorage` usable: a new random key, written as above.
  - A plain key file (`"none"`) and `safeStorage` usable: **the same key** moves into `safeStorage`, so the sealed secrets still open.
  - `safeStorage` not usable and no key file of its own: the core keeps or creates its plain file, as before, and both the app and the core log a warning.
- **In the core**:
  - `musubee-core -key-from-stdin` reads the key from the first line of its input (base64, 32 bytes) before serving the API, and passes it as `database_key`; the log says `"protection":"host"`. A missing or invalid line stops it with an error that does not quote the line. A key that does not open the database stops it as in ADR 0019 (`ErrWrongKey`).
  - `secrets.LoadOrCreate` refuses a key file with the protection `electron-safestorage` (`ErrKeyHeldByApp`) and leaves it unchanged. The core started without the app cannot replace the key.
- **CI**: the desktop end-to-end tests run on macOS with the runner's Keychain, and on Linux in a D-Bus session of their own with an unlocked GNOME keyring and `XDG_CURRENT_DESKTOP=GNOME`. `MUSUBEE_REQUIRE_SAFESTORAGE=1` makes a fallback to the plain file a failure there, not a skip. Playwright 1.64's Electron loader adds `--password-store=basic` and `--use-mock-keychain` to every app it starts (`playwright-core/lib/server/electron/loader.js`): `safeStorage` then uses a hardcoded password on Linux and a mock Keychain on macOS, and the first CI runs checked those, not the system's storage. With `MUSUBEE_E2E=1` the app removes both switches before it is ready; a test that wants a plain password store sets `MUSUBEE_E2E_PASSWORD_STORE=basic`.

### Threat model (changes to ADR 0019's)

| Attacker | Protected? |
|---|---|
| A copy of the data directory alone | **Yes** now on macOS, and on Linux with a password store: the key file opens only through the user's Keychain or keyring. **No** on a Linux desktop without a password store, which keeps the plain file, as before. |
| Another program running as the same user, on macOS | **Mostly**: the Keychain asks the user before another application reads the app's item ("without user override", Electron's documentation). It can still read the core's memory or impersonate the app in other ways. |
| Another program running as the same user, on Linux | **No**: the Secret Service gives any program of the unlocked session the items it asks for. |
| Root, or malware with the user's rights while the app runs | **No**, as in ADR 0019. |

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| Electron 44.7.0's `safeStorage` has `isEncryptionAvailable`, `encryptString`, `decryptString` and, on Linux, `getSelectedStorageBackend`, whose `basic_text` means no password store | **verified** (type definitions of the installed Electron, and its documentation) | `apps/desktop/node_modules/electron/electron.d.ts`; https://www.electronjs.org/docs/latest/api/safe-storage (2026-10-11) |
| Under `basic_text`, data is "encrypted via hardcoded plaintext password"; the asynchronous API's Linux fallback cannot be detected | **verified** (documentation) | same page, 2026-10-11 |
| The core takes its key from its input, keeps no key file then, logs no key, and another key stops it | **verified** (Windows) | `core/cmd/musubee-core/main_test.go`, `TestKeyFromStdin`, `TestKeyFromStdinRefusesABadKey` |
| The core refuses, and keeps, a key file of `safeStorage` | **verified** | `core/secrets/secrets_test.go`, `TestKeyFileOfTheApp` |
| The supervisor gives the key at every start, restarts included | **verified** (Windows, the real core) | `apps/desktop/test/core-process.test.ts` |
| Creating, opening and moving the key, and stopping on a key that does not open, with a stand-in for `safeStorage` | **verified** (unit tests, Windows) | `apps/desktop/test/core-key.test.ts` |
| With the real `safeStorage`, a new data directory gets a key file of `safeStorage`, the account opens again after a restart, and the log has no key | **verified** on CI (run 38171306889, with Playwright's switches removed and `MUSUBEE_REQUIRE_SAFESTORAGE=1`): macOS with the runner's login Keychain, Linux with the GNOME keyring; Windows, which keeps DPAPI, is **verified** locally and on CI | `apps/desktop/e2e/app.spec.ts` |
| A plain key file moves into the GNOME keyring with the same key, and the account stays | **verified** on CI's Linux runner (run 38171306889; `MUSUBEE_E2E_PASSWORD_STORE=basic` for the first start) | `apps/desktop/e2e/app.spec.ts` |
| Playwright 1.64 starts Electron with `--password-store=basic` and `--use-mock-keychain`, and the app started directly on the Linux runner with an unlocked GNOME keyring gets a key file of `safeStorage` | **verified** (Playwright's source; a probe run on GitHub's Linux runner, 2026-10-11) | `apps/desktop/node_modules/playwright-core/lib/server/electron/loader.js` |
| macOS asks for Keychain access again after each update of an app without a stable code signature | **assumed** (Electron's documentation); to check when the app is packaged and signed (E3.2) | Electron's documentation, 2026-10-11 |

## Consequences

- On macOS, and on Linux with a password store, a copy of the data directory no longer holds a usable key. That closes ADR 0019's gap on the desktop, except on Linux without a password store, which keeps the plain file and its warning.
- The core program alone can no longer open such a data directory. Tools and tests that run it directly use a directory of their own, or pass `-key-from-stdin`.
- Losing access to the Keychain item or the keyring (a reset keyring, a data directory copied to another machine) loses the accounts, as a lost DPAPI or Keystore key does. Recovering from a lost key in the interface is E2.2.
- A plain key file replaced by the move may remain in free disk blocks: the move does not erase it securely, which a file system cannot promise anyway.
- Follow-ups: the asynchronous API when it can report a fallback; the interface could show that the key is not in a secure storage (the status, today only in the logs); the Keychain prompts of a signed app (E3.2).

## Sources

- Electron, `safeStorage` documentation: https://www.electronjs.org/docs/latest/api/safe-storage (2026-10-11).
- Electron 44.7.0, `electron.d.ts`, `interface SafeStorage` (installed in `apps/desktop/node_modules`, 2026-10-11).
- ADR 0013 (desktop shell), ADR 0019 (secrets at rest), ADR 0026 (Signal's secrets at rest).
