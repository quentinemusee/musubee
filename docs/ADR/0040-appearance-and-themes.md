# ADR 0040 — Appearance: design tokens set by root attributes, kept per device in the page's storage

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.24
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

The product vision (`CLAUDE.md` §1) promises "maximum customization (themes, appearance, layout)". E2.7 starts with themes, app-wide. `ui/src/styles.css` already had design tokens (`--color-*`, `--radius`, `--space`) with a dark set under `prefers-color-scheme: dark`, but fixed font sizes in pixels and no way for the user to choose.

Three questions: how a choice reaches the styles; where it is kept; what to offer first.

Sources: the CSS Custom Properties specification (a custom property may reference another set on the same element; `var()` resolves at computed-value time); WCAG 2.2, success criteria 1.4.3 (contrast 4.5:1) and 1.4.4 (resize text); Electron 44, `protocol.registerSchemesAsPrivileged` (a `standard`, `secure` scheme gets its own storage), as `apps/desktop/src/main.ts` registers `app://`; HTML, `<dialog>` and `showModal`.

## Options considered

### How a choice reaches the styles

#### Option A — Data attributes on the root element (chosen)
`data-theme`, `data-accent` and `data-text-size` on `<html>`; `styles.css` redefines the tokens under attribute selectors. No inline style (the Content-Security-Policy has `style-src 'self'`, ADR 0013), no runtime CSS generation.
- Accents are pairs (`--accent-light`, `--accent-dark`, `--mine-light`, `--mine-dark`); each theme picks its half. CSS's `light-dark()` would say it shorter, but the Android app accepts WebView 111 and later (ADR 0021), and `light-dark()` needs Chromium 123.
- Font sizes become multiples of `--text` (1px, 0.9px, 1.15px or 1.3px): the text grows, spacing does not.

#### Option B — Inline custom properties set from script
- Cons: free colors are easy to make unreadable; nothing that a fixed set of tested pairs does not do for now.

### Where the choice is kept

#### Option C — The page's storage, per device (chosen)
`localStorage` of the app's origin (`app://musubee` on the desktop, the WebView's on Android), read before the first render so that the app never flashes in another theme.
- Pros: nothing private (no message, no account); works before the core starts, and even when it cannot (a lost key, ADR 0031).
- Cons: not synced between the user's devices; cleared with the app's web data.

#### Option D — The core's database
- Pros: syncable later with the persons (ADR 0017).
- Cons: the theme would wait for the core, and a core that fails to start would show the default; syncing settings is a later decision.

### What to offer first

The theme (same as the system, light, dark), five accents and four text sizes. Layout options (density, inbox width) and custom colors come later, on the same tokens.

## Decision

**Options A and C**, with the first set of choices.

- `ui/src/appearance.ts`: the choices, parsing (field by field, defaulting what is missing or unknown, so that a newer or hand-edited value never breaks the app), loading and saving (a storage that throws keeps the default for the run), applying to the root element.
- `ui/src/styles.css`: the tokens above; the dark theme when chosen, or when the system is dark and the user follows it (`:root:not([data-theme="light"])` inside the media query).
- The five accents, measured: white text on the light accent reaches 7.08 (indigo), 5.47 (teal), 6.29 (rose), 5.02 (amber), 7.58 (slate); the dark background's color on the dark accent 6.21, 12.3, 9.62, 12.62, 12.26 — all at least 4.5:1.
- `ui/src/app/AppearanceSettings.tsx`: an "Appearance" button in the header opens a modal `<dialog>` with three groups of radio buttons, applied at once; Escape and Done close it.

## Consequences

- Since T2.41, a density (comfortable, compact) is a fourth choice: compact lowers `--space` and the bubbles' padding.

- The text size scales up to 1.3 times; WCAG's 200% resize is still the browser's or the system's zoom (Electron's Ctrl+plus, Android's font scale through the WebView).
- Each device keeps its own appearance. Syncing settings, if wanted, is a later ADR.
- Every new color in `styles.css` must be a token, or it will not follow the theme.
- iOS (later) uses the same storage in WKWebView; nothing here is specific to a shell.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The appearance is read, written, defaulted field by field, survives a storage that throws, and applies as root attributes | **implemented, unverified** | `ui/src/appearance.test.ts` |
| The desktop app applies a theme, an accent and a text size at once, closes the dialog with Escape, and keeps the choice after a restart | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "the user picks a theme, an accent and a text size, kept after a restart" |
| The accents' contrasts | **verified** (computed with the WCAG formula) | the figures above |
| The Android app keeps the appearance | **assumed** (the WebView's storage persists by default) | |
