# ADR 0049 — Interactive verification: comparing emoji with another of the user's devices

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.43
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

ADR 0044 lets a Matrix account verify Musubee with its recovery key. Many users have no recovery key at hand but have another client signed in, such as Element on their phone. Matrix's other way of proving a new device is interactive verification. Both devices show the same short authentication string (SAS), and the user compares it. The specification defines it in the client-server API, "Key verification framework" and "Short Authentication String (SAS) verification". It also defines a QR code variant.

mautrix-go v0.31.0 ships `crypto/verificationhelper`, read in the module cache:
- `NewVerificationHelper(client, mach, store, callbacks, supportsQRShow, supportsQRScan, supportsSAS)` and `Init(ctx)`, which registers its handlers on the client's `ExtensibleSyncer`;
- `StartVerification(ctx, userID)` sends a request to every device of the user;
- `AcceptVerification`, `StartSAS`, `ConfirmSAS` and `CancelVerification` act on one transaction;
- the callbacks report each step: `VerificationRequested`, `VerificationReady`, `VerificationCancelled`, `VerificationDone`, and `ShowSAS`, which gives the emoji and the decimals;
- when a verification succeeds, the helper signs the other device with the user's self-signing key, if it holds one. It also marks the other device's key trusted.

## Options considered

### Which method

#### Option A — Emoji (SAS) only (chosen)
- Pros: every Matrix client offers it; it needs no camera and no QR code drawing on the Musubee side.
- Cons: comparing seven emoji is slower than scanning.

#### Option B — QR codes as well
- Cons: scanning needs the camera on every shell (a permission on Android and on macOS), and showing needs the reciprocate flow. Left for later. Musubee already draws QR codes (T2.13), so showing one is the smaller half.

### Whose devices

#### Option C — The user's own devices only (chosen)
A request from another user is cancelled at once. Verifying other people is a different feature, with a different interface: a person's trust, not an account's.

#### Option D — Other users too
- Cons: a new question in the interface ("is this really Alice?"), which the product does not ask yet.

### How the interface follows it

#### Option E — One command, one event (chosen)
- `accounts.verification` takes an account and an action: `start`, `accept`, `confirm` or `cancel`.
- `account.verification` reports the step: `incoming`, `waiting`, `emoji` with the seven emoji and their names, `confirmed`, `done` or `cancelled` with a reason.
- One verification per account at a time: the core keeps the current transaction, so the actions name no transaction.

## Decision

**Options A, C and E**, API 1.22.

- `core/connector/matrix/verification.go` wraps the helper:
  - it is started after the recovery check (`setUp`), and only for an account that can verify;
  - `Ready` starts SAS;
  - `Done` marks the account verified, which ADR 0044's indicator shows;
  - every step goes to `Config.OnVerification`.
- `core/embedded/recovery.go` turns it into the event, and dispatches the command. A network without verification answers `unsupported`; an action without a verification in progress answers `invalid_params`.
- The interface (`ui/src/app/EmojiVerification.tsx`):
  - an unverified Matrix account offers "Compare emoji…";
  - a request from another device shows "Another device asks to verify this one…";
  - the dialog shows each step, with "They match" and "They do not match";
  - closing it before the end cancels the verification on both devices.

## Consequences

- An account verified this way opens its encrypted history only once the other device shares its keys, or once key backup is opened with the recovery key (ADR 0044). The emoji prove the device; they do not move keys by themselves.
- Musubee must hold the self-signing key to sign the other device. Without it, the other device signs Musubee and Musubee only trusts the other device locally. This is the usual outcome for a new device.
- QR codes and verifying other people are later tasks.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| Two devices of one Synapse user verify each other by emoji; Musubee reports waiting, emoji, done and the account becomes verified | **implemented, unverified** | `infra/matrixtest/verification_integration_test.go`, `TestMatrixInteractiveVerification` (CI only, needs Docker) |
| Networks without verification answer `unsupported` | **implemented, unverified** | `core/embedded/recovery_test.go`, `TestVerifyNeedsAMatrixAccount` |
| The dialog follows the steps and cancels when closed early | **assumed** (typecheck and lint only) | `ui/src/app/EmojiVerification.tsx` |
