# ADR 0047 — Desktop notifications: the main process announces recent messages while the app is in the background

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.33
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

The Android app announces new messages from its service (ADR 0033); the desktop app announced nothing: a message arriving while the window was in the background went unnoticed. The interface already has a shell interface for notifications (`ShellNotifications` in `ui/src/shell.ts`: `ask`, `onOpen`), which the desktop preload did not provide.

Sources: Electron 44 `Notification` (main process; `isSupported`, `show`, `close`, the `click` and `close` events), `BrowserWindow.isFocused`; the desktop shell's permission rules (ADR 0038 grants the page nothing but the microphone, so the page's own Web Notifications would be refused).

## Options considered

### Option A — The main process decides and shows, from the core's events (chosen)
The main process already receives every event of the core to pass it to the windows. It announces a `message.added` from someone else, not deleted, at most five minutes old, while no window of the app is focused; one notification per conversation, which the next replaces; a click restores and focuses the window and opens the conversation (`notifications:open` to the preload's `onOpen`).
- Pros: works while the page is busy or the window hidden; no permission for the page; the same place as the other shell decisions.
- Cons: the main process does not know the conversation names: the title is the sender's name.

### Option B — The page uses Web Notifications
- Cons: a permission to grant the page; the page decides while it may be throttled in the background.

## Decision

**Option A.** `apps/desktop/src/notification-rules.ts` holds the rules (no Electron import, tested under Node); `main.ts` shows the notifications; the preload exposes `notifications` (`ask` resolves true: desktop systems need no permission; `onOpen` passes only strings).

- Five minutes, not a day as on Android: history that comes in batches (ADR 0041) is published as new messages, and a login's latest messages must not each make a notification.
- The text is shown as on Android (sender, then the text or "Photo: caption"); a setting to hide it is left for later.

## Consequences

- No notification while a window is focused, even on another conversation: the inbox's unread badges say it.
- On Linux, notifications need a notification daemon; without one, nothing shows (Electron's `isSupported`).
- Since T2.44 (ADR 0050), the main process learns the conversations' names, kinds and mutes from the core's events and answers. Muted conversations are skipped, and a group's title is "Alice in Book club".

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The rules: recent messages from others in the background only; the wording of media | **implemented, unverified** | `apps/desktop/test/notification-rules.test.ts` |
| Notifications show, replace each other per conversation, and a click opens the conversation | **unknown** (needs a desktop session; to try by hand on Windows) | |
