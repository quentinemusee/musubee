# ADR 0066 — Links in messages: web addresses only, opened outside the app by the shell

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.67
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

Messages showed addresses as plain text. Making them links touches security (`CLAUDE.md` §6.8, ADR 0013):
- the desktop app's pages must never navigate;
- no new window may open;
- an address in a message comes from anyone, so a link must not run code or reach local files.

Sources: Electron 44, "Security" checklist ("Do not use shell.openExternal with untrusted content" without validating it; "Disable or limit navigation"; "Disable or limit creation of new windows"), `shell.openExternal`, `webContents` `will-navigate`; Capacitor 8, Android `WebViewClient`: navigations to hosts other than the app's are handed to the system.

## Options considered

### Which addresses

`http` and `https` only, and `www.` addresses as `https`. Each link shows its address as written, so the user sees where it goes. Final punctuation and an unbalanced closing parenthesis are not part of an address.

### How the desktop opens them

#### Option A — The interface asks the main process (chosen)
A click on a link calls the preload's `links.open`. The main process checks the address again (web addresses only, no user name or password in it, never the app's origin) and calls `shell.openExternal`. Navigations stay refused, so neither a script nor a redirect can take the page anywhere.

#### Option B — Open the target of every navigation
- Cons: a script setting `location.href` would open the system's browser without the user's click.

### Android

The WebView hands navigations to other hosts to the system (Capacitor's default), so the interface lets the link be followed there.

## Decision

**Option A** on the desktop, Capacitor's default on Android.

## Consequences

- No previews of links: fetching a page would tell its site that the message was read, and from where.
- `mailto:` and phone numbers are not links yet.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| Addresses found, punctuation and parentheses, nothing but http and https | **implemented, unverified** | `ui/src/links.test.ts` |
| The main process opens web addresses only | **implemented, unverified** | `apps/desktop/test/external-links.test.ts`; `apps/desktop/e2e/app.spec.ts`, "the interface is isolated" (refuses `file:`) |
| A message shows its address as a link | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "a web address in a message is a link" |
| Android opens a link in the system's browser | **assumed** (Capacitor's default; to try by hand) | |
