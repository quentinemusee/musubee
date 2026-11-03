# ADR 0068 — Saving files on Android: the system's document picker, through the plugin

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.76
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

The interface saves a file (a received file, ADR 0036; an exported conversation, T2.75) as a Blob behind a link with `download`. Electron saves it with its dialog. Android's WebView, as Capacitor sets it up, has no download listener, so the same click does nothing on Android.

Sources: Android's developer guide, "Access documents and other files from shared storage" (`Intent.ACTION_CREATE_DOCUMENT`, no storage permission needed, the user chooses the place); Capacitor 8, `Plugin.startActivityForResult` and `@ActivityCallback`.

## Options considered

#### Option A — The document picker, through the plugin (chosen)
The plugin's `saveFile({name, mimeType, data})` opens `ACTION_CREATE_DOCUMENT` with the name. The user chooses where; the plugin then writes the bytes, decoded from base64, to the chosen document, on a background thread.
- Pros: no permission, on every Android version the app supports (API 24 and later); the user sees and chooses where the file goes.
- Cons: the bytes cross the bridge in base64, a third more; media are at most 100 MiB (ADR 0037).

#### Option B — The Downloads collection through MediaStore
- Cons: API 29 and later only, and older versions need the storage permission.

#### Option C — Sharing the file to another app
- Cons: a different gesture from "save"; a later option.

## Decision

**Option A**:
- `ShellFiles.save` is optional, like the other shell features;
- `ui/src/save.ts` saves through it when the shell has it, and as a download otherwise;
- received files and exports both go through `saveBlob`.

## Consequences

- The file's content is never logged; a failure logs its exception's kind only.
- The desktop is unchanged.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The mobile shell hands the name, type and base64 bytes to the plugin | **implemented, unverified** | `ui/src/capacitor.test.ts` |
| The plugin's Kotlin compiles | **verified** (cloud session, `:app:compileDebugKotlin` without the Go core) | |
| A file and an export are saved where the user chose | **unknown** (to try by hand on the Pixel 8 Pro) | |
