# ADR 0067 — Pausing notifications: a time kept by the shells, next to the preview

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.68
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

Muting (ADR 0050) is per conversation. During a meeting or a night, the user wants every notification to wait, then come back by itself. The systems' own "Do not disturb" exists but is all or nothing for every app.

## Options considered

### Where the pause is kept

#### Option A — By the shells that post notifications, next to the preview (chosen)
The Android service and the desktop's main process decide every notification (ADR 0033, 0047), and keep the preview already (ADR 0051): `notifications.json` on the desktop, the `notifications` preferences on Android. A pause is a time in Unix milliseconds; 0 is none.

#### Option B — The core
- Cons: as for the preview, the notifiers would ask the core before each notification.

### Schedules

Quiet hours every night are a later option. A pause that ends by itself covers the common case and cannot be forgotten.

## Decision

**Option A**:
- the interface's notifications dialog offers "For 1 hour", "For 8 hours" and "Until tomorrow morning" (8:00 in the device's time), says until when, and offers "Resume now";
- `ShellNotifications.pausedUntil` and `pauseUntil` are optional, like the preview's;
- the desktop's main process refuses anything but a non-negative whole number;
- Android's plugin gains `notificationsPausedUntil` and `pauseNotifications`.

## Consequences

- Messages that arrive during a pause count as unread, and their notifications are not shown afterwards: the inbox says enough.
- A mention of the user (ADR 0055) waits too: a pause is the user's explicit choice.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The desktop keeps the pause with the preview, and a pause ends by itself | **implemented, unverified** | `apps/desktop/test/notification-settings.test.ts` |
| The mobile shell reads and keeps it through the plugin | **implemented, unverified** | `ui/src/capacitor.test.ts` |
| The choices' times | **implemented, unverified** | `ui/src/notification-preview.test.ts` |
| The desktop app pauses, keeps it after a restart, and resumes | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "the user chooses what notifications show" |
| The app's Kotlin compiles | **verified** (`:app:compileDebugKotlin`, as in ADR 0064) | |
| Android notifications wait | **unknown** (to try by hand) | |
