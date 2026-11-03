# ADR 0051 — What notifications show: the user chooses, the shell that posts them keeps it

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.45
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

Notifications show the sender and the text of new messages. On Android this happens when the screen is unlocked (ADR 0033); on the lock screen, the public version shows "New message" only. On the desktop it happens whenever no window is focused (ADR 0047). ADR 0047 left "a setting to hide it" for later. The product promises end-to-end encryption; a notification on a shared screen, or on a screen recording, undoes some of it. Signal offers the same three choices: name and message, name only, no name or message.

Questions: what the choices are; who keeps them; who applies them.

## Options considered

### Who keeps the choice

#### Option A — The shell that posts the notifications (chosen)
The Android service and the desktop's main process post the notifications. They must know the choice without the interface: the Android service runs while no page is loaded, and the main process announces messages while every window is in the background.
- Android keeps it in the app's `SharedPreferences` (`notifications`, key `preview`), next to the accounts' watermarks.
- The desktop keeps it in `notifications.json` in the app's data directory. It is written whole through a temporary file, read at start-up, and changed through IPC that refuses unknown values (`ADR 0013`'s validation rule).
- Pros: no core change; the choice applies before the core starts.
- Cons: each device keeps its own, like the appearance (ADR 0040).

#### Option B — The core's database
- Cons: the Android service would wait for a core call before each notification; the desktop's main process does not call the core itself.

#### Option C — The page's storage
- Cons: the native code cannot read it.

### What "no name or message" shows

The title "Musubee" and "New message". The notification stays one per conversation, so that edits and deletions still find their lines (ADR 0034). Tapping it still opens the conversation.

## Decision

**Option A**, with three choices, named as the interface shows them: "Name and message" (the default, as before), "Name only", "No name or message".

- `ShellNotifications.preview()` and `setPreview()` are optional. A shell without them offers no choice: the header's "Notifications" button appears only when the shell has them.
- Android: `MessageNotifications.hide` applies the choice to a post; `MessageNotifier` reads the preference for each post; the plugin gains `notificationPreview` and `setNotificationPreview`.
- Desktop: `hide` in `notification-rules.ts`; `notification-settings.ts` keeps the file; the preload exposes both methods.

## Consequences

- The lock screen's public version on Android is unchanged: it never shows names or text.
- A notification already shown keeps its content until it is replaced.
- The interface says plainly that anyone who sees the screen can read what notifications show.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| Android hides the text, or the names too, and keeps the messages' IDs | **implemented, unverified** | `MessageNotificationsTest.aPreviewHidesTheTextOrEverything` |
| The desktop hides the same way, and keeps the choice in its data directory | **implemented, unverified** | `apps/desktop/test/notification-rules.test.ts`, `apps/desktop/test/notification-settings.test.ts` |
| The mobile shell reads and keeps the choice through the plugin | **implemented, unverified** | `ui/src/capacitor.test.ts` |
| The desktop app offers the choice, refuses unknown values from the page, and keeps it after a restart | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "the user chooses what notifications show" |
| Android notifications follow the choice on a device | **unknown** (to try by hand on the Pixel 8 Pro) | |
