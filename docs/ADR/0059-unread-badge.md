# ADR 0059 — The unread count on the desktop app's icon: counted by the main process

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.56
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

A messenger in the background says on its icon that messages wait. Electron's `app.setBadgeCount` shows a count on macOS's Dock and on Linux launchers that implement Unity's launcher API; on Windows it does nothing (a taskbar overlay icon would need drawn images, a later task).

Sources: Electron 44, `app.setBadgeCount` (returns whether it worked; supported on macOS and Linux).

## Decision

The main process counts, from the conversations it already learns for the notifications (ADR 0047, 0050): the core's events and its answers to the interface.
- A conversation counts its unread messages; a conversation marked unread with none counts one.
- Muted conversations do not count, as they do not notify.
- Deleted conversations are forgotten.

The count is set when it changes.

## Consequences

- Only conversations the interface has read (the inbox's pages) or that changed since the start are counted; an unread conversation beyond the loaded pages counts once it changes.
- Windows shows no count yet.
- The count holds no message content.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The count, without muted or deleted conversations | **implemented, unverified** | `apps/desktop/test/notification-rules.test.ts` |
| The Dock shows it on macOS | **unknown** (to try by hand) | |
