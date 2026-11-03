# ADR 0058 — Keyboard shortcuts: few, conventional, and never in a dialog's way

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.54
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

Desktop users move between conversations all day. The app could be driven by Tab and Enter only (the message menu, ADR 0035; the dialogs), but reaching the search or the next conversation took many presses. Shortcuts must not take keys that the system, the browser engine or assistive technologies use, nor keys that text fields need.

Sources: WCAG 2.2, success criterion 2.1.4 (character key shortcuts: shortcuts made of a single character must be avoidable; ours all use a modifier); the conventions of other messengers (Ctrl+K for "go to" in Slack, Discord and Telegram Desktop; Alt+Up and Alt+Down to move between chats in Slack, Discord and Telegram Desktop).

## Decision

- Ctrl+K (Cmd+K on macOS) focuses the message search (ADR 0046).
- Alt+Up and Alt+Down open the previous and next conversation in the inbox's order; with none open, Alt+Down opens the first. Alt with an arrow moves nothing in a text field, so they work while writing.
- Shortcuts wait while a modal dialog is open, and leave alone a key press that something else already handled.
- `ui/src/shortcuts.ts` holds the rules as pure functions; `ui/src/app/Shortcuts.tsx` listens on the window.

## Consequences

- No single-character shortcuts, so nothing to turn off (WCAG 2.1.4).
- The shortcuts are not listed in the app yet; a help panel is a later task.
- On Android, hardware keyboards get them too, through the WebView.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| Which key presses are shortcuts; which conversation opens | **implemented, unverified** | `ui/src/shortcuts.test.ts` |
| The desktop app moves between conversations and focuses the search | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "keyboard shortcuts reach the search and the next conversation" |
