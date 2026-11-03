# ADR 0038 — Recording voice messages: MediaRecorder in the page, the microphone the only permission, the container the network takes

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.22
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

T2.21 (ADR 0037) sends media from files. The product vision (`CLAUDE.md` §1) names voice messages first: the user records one in the app and sends it.

Three questions:

1. Who records: the page (the browser's `MediaRecorder`) or the shells (Android's `MediaRecorder`, a native module on the desktop)?
2. What may the page do with the microphone? The desktop shell refused every permission (ADR 0013); Android declared none for media.
3. In which format? Networks take voice messages in some containers only, and bridgev2 refuses a message whose type a network does not list.

What the connectors take for voice messages (`event.CapMsgVoice` in their room features, read in the module cache):

| Network | Types |
|---|---|
| Telegram (mautrix-telegram v0.2609.0, `pkg/connector/capabilities.go`) | `audio/ogg`, `audio/mpeg`, `audio/mp4` |
| Signal (our copy of mautrix-signal v0.2609.0, `pkg/connector/capabilities.go`) | `audio/aac`; `audio/ogg` only with ffmpeg (`supportedIfFFmpeg`), which the app does not ship |
| Matrix (our connector) and echo | any |

What Chromium's `MediaRecorder` records (Electron 44, Android System WebView 111 and later): WebM (Opus) and MP4 (Opus or AAC); never Ogg nor raw AAC.

Sources: mautrix-go v0.31.0 `bridgev2/portal.go` (`checkMessageContentCaps`: a type the network lists as rejected, or does not list without `*/*`, is refused), `event/capabilities.go` (`GetMimeSupport`, `CapabilitySupportLevel.Partial`); `@capacitor/android` 8.5.3 `BridgeWebChromeClient.onPermissionRequest` (asks Android for `RECORD_AUDIO` and `MODIFY_AUDIO_SETTINGS` on an audio capture request, grants the page's request only if the user does); Electron 44 `session.setPermissionRequestHandler` and `setPermissionCheckHandler` (`MediaAccessPermissionRequest.mediaTypes`, `PermissionCheckHandlerHandlerDetails.mediaType`, `isMainFrame`); the W3C MediaStream Recording specification.

## Options considered

### Who records

#### Option A — The page, with `MediaRecorder` (chosen)
- Pros: one implementation for the desktop and Android (and iOS's WKWebView, which has `MediaRecorder` since iOS 14.3); the recording is a `Blob` that goes through T2.21's upload unchanged.
- Cons: the container is what the web view records, not what a network prefers.

#### Option B — Each shell records natively
- Pros: any format the platform encodes (AAC on Android, Ogg on none without a library).
- Cons: three implementations, a new bridge API per shell, the bytes crossing the bridge anyway.

### The format

#### Option C — The first container both the web view records and the network takes (chosen)
API 1.11 adds `MediaAction.mime_types`, the types a network takes for each kind (from its room features, at least partly supported). The interface asks for, in order, Ogg Opus, MP4 (Opus, then any codec), WebM (Opus, then any), keeping the first the web view records and the network takes for voice messages (else for audio). When none fits, it records the web view's first type, and the message goes as audio or as a file if the network takes that type for those kinds; otherwise the user is told before anything is sent.
- On Telegram: MP4 from Chromium, a voice message.
- On Signal: no Chromium container fits `audio/aac`: the recording goes as audio or a file if Signal takes it so, not as a voice message. A real voice message on Signal needs the core to repackage AAC from MP4 into ADTS (a remux, no encoding): a later task.

#### Option D — Transcode in the core to each network's preferred format
- Cons: an Opus or AAC encoder in Go (cgo, or ffmpeg, which the app does not ship); the size and licensing of that dependency.

### The permission

#### Option E — Only the microphone, only for the app (chosen)
- Desktop: the permission handlers grant `media` when every requested type is `audio`, for the main frame of `app://musubee`; the camera, the screen, notifications and everything else stay refused (`apps/desktop/src/permissions.ts`). A custom scheme's `URL.origin` is `"null"`, so the scheme and host are compared.
- Android: the manifest declares `RECORD_AUDIO` and `MODIFY_AUDIO_SETTINGS`; Capacitor asks the user the first time the page asks. No camera permission is declared, so a page asking for the camera is refused by Android.

## Decision

**Options A, C and E.**

- **Interface** (`ui/src/recorder.ts`, `ui/src/app/VoiceRecording.tsx`):
  - the composer shows a microphone button ("Record a voice message") in place of Send while there is nothing to send and the conversation takes media;
  - while recording, the composer shows a pulsing dot (still when the user prefers reduced motion), the elapsed time (`role="timer"`), Cancel and "Send voice message", which takes the focus;
  - the microphone opens when recording starts (echo cancellation and noise suppression on, no video) and closes when it ends, whatever ends it: sent, cancelled, the composer going (another conversation, a closed window), or the 15-minute limit;
  - chunks every second, so that a long recording is not one buffer; a cancelled recording is thrown away;
  - the recording goes through `sendMedia` (ADR 0037) as a voice message with its duration, named `voice-YYYYMMDD-HHMMSS.<ogg|m4a|webm>`;
  - a refused microphone says so ("allow it in the system's settings").
- **API 1.11**: `MediaAction.mime_types`.

## Consequences

- Telegram gets voice messages in MP4 from the desktop and Android: **assumed** to show as voice messages there, not run against Telegram in T2.22.
- Signal gets no voice messages from Musubee until the AAC repackaging: its room features take `audio/aac`, `audio/mpeg`, `audio/mp3` and `audio/flac` as audio, and any type as a file, so a recording in MP4 goes as a file (read in our copy of mautrix-signal, `pkg/connector/capabilities.go`).
- On Android the first recording asks for the microphone; a user who refuses for good must allow it in the system's settings, which the error says.
- iOS (later) records in MP4 AAC with WKWebView; nothing here is specific to Chromium but the order of containers.
- A browser without `MediaRecorder` shows no microphone button.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The recording type is the first both the web view and the network take; names; chunks; the duration; the microphone closes on stop, cancel and the limit; a refusal is passed on | **implemented, unverified** | `ui/src/recorder.test.ts` |
| A network's media types are followed when choosing a kind | **implemented, unverified** | `ui/src/media.test.ts` ("follows the media types the network lists") |
| The core lists the types a network takes, none as no kind | **implemented, unverified** | `core/embedded/upload_test.go` `TestMimeTypes` |
| The desktop grants the microphone to the app's main frame only, nothing else | **implemented, unverified** | `apps/desktop/test/permissions.test.ts` |
| The desktop app records with Chromium's fake microphone, cancels, sends a voice message that the echo network sends back, and the camera stays refused | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "the user records a voice message and sends it" |
| Android asks for the microphone and records | **unknown** (not tried; to check by hand on the Pixel 8 Pro) | |
| Telegram shows the voice messages as such | **assumed** | |
