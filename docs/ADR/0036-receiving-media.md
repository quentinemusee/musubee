# ADR 0036 — Receiving media: bytes read in parts through the API, shown from Blobs of safe types only

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.20
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

E2.4 asks for photos, videos, voice messages, GIFs and files in the thread. Until T2.20 every such message reached the interface as `unsupported`, showing its file name: the API only had text.

On the core's side the media is already on the device. bridgev2 connectors upload a received photo through `MatrixAPI.UploadMedia` (or `UploadMediaStream`), which the local server stores in `local_media` (ADR 0009), and the message's content names it with an `mxc://` URI (`event.MessageEventContent.URL`, `Info`, `FileName`). A native Matrix account (ADR 0018) is different: its connector passes the homeserver's content along, so the media stays on the homeserver, encrypted when the room is.

Three questions:

1. How does the interface get the bytes? The API is JSON documents (ADR 0012), carried in-process over JNI on Android and over the child process's stdio on the desktop (ADR 0013). There is no HTTP server, and the desktop window has no file access.
2. How does the interface show bytes that a stranger chose, including their media type, without letting them run as part of the app?
3. When is media read: every photo of a long thread, or on demand?

Sources: mautrix-go v0.31.0 `event/message.go` (`MessageEventContent`, `FileInfo` with `MauGIF` and `IsAnimated`, `GetCaption`, `GetFileName`, `MSC3245Voice`, `MSC1767Audio`), `event/message.go` `GetCapMsgType` (a GIF is an `m.video` with `fi.mau.gif`), `crypto/attachment/attachments.go` (`EncryptedFile.DecryptInPlace`, which checks the SHA-256 before decrypting), `client.go` (`Client.Download`, authenticated media); the Matrix client-server specification, "m.room.message msgtypes" (`body` is the caption when `filename` is set) and "Send-to-device messaging / Sending encrypted attachments"; SQLite's `substr` on BLOBs (bytes, 1-based); the HTML specification on `blob:` URLs, which take the origin of the document that created them.

## Options considered

### How the bytes reach the interface

#### Option A — `media.read`, parts of at most 1 MiB in base64 (chosen)
One command reads a range of a media: `media_id`, `offset`, `length` (default and maximum 1 MiB); the answer has the type, the whole size and the bytes in base64. The interface reads part after part and builds a `Blob`.
- Pros: works on every transport as it is; no new channel, no new permission; a large video is never one huge JSON document, and SQLite reads only the range asked (`substr` on the BLOB).
- Cons: base64 costs a third more bytes and a copy; a 50 MB video is 50 calls.

#### Option B — A local HTTP server in the core
- Cons: a listening socket on every platform, its own authentication (any local process could connect), a CSP opening to `http://127.0.0.1:port`; on Android, cleartext traffic rules. Much more to secure than the gain.

#### Option C — The shell writes media to files and the page loads them (`file://`, Capacitor's `convertFileSrc`)
- Cons: different on each shell; plaintext media files outside the database; on the desktop, the app protocol (ADR 0013) would have to serve a second directory.

### How media shows

#### Option D — Blobs of an allowed type only (chosen)
The interface shows inline only image, video and audio types that browsers decode without running anything: PNG, JPEG, GIF, WebP, AVIF, BMP; MP4, WebM, Ogg, QuickTime; Ogg, MPEG, MP4, AAC, WAV, WebM, FLAC, Opus audio. The Blob takes that type, through an object URL in `<img>`, `<video>` or `<audio>`. Everything else (SVG included: it is code) is a file: a Blob of `application/octet-stream` behind a link with `download`, never a navigation. A `blob:` URL has the app's origin: a Blob typed `text/html` or `image/svg+xml` and opened as a page would run with the app's rights, which this rule rules out.
- The Content-Security-Policy gains `img-src blob:` and `media-src blob:`, nothing else (`ui/index.html`, `apps/desktop/src/app-protocol.ts`).

#### Option E — `data:` URLs
- Cons: a whole media in one string, in the DOM; `data:` media needs `media-src data:`; no gain over Blobs.

### When media is read

#### Option F — Small media as soon as its row renders, larger on request (chosen)
Media up to 8 MB is read when its message is rendered; the thread is virtualized (ADR 0020), so that is only near the viewport. Larger media shows a button with its size ("Load video (48 MB)"), over its thumbnail when there is one. Read media is cached as Blobs, the latest used kept up to 64 MB; a read in progress is shared.

## Decision

**Options A, D and F.**

- **API 1.9** (`core/api/schema/core-api.schema.json`):
  - `MessageKind` gains `image`, `video`, `audio`, `voice` and `file`; for these, `text` is the caption (often empty), and `Message.media` describes the media: `media_id`, `mime_type`, `file_name`, `size`, `width`, `height`, `duration_ms`, `animated`, `thumbnail_id`. A deleted message has no media.
  - `animated`: a GIF, whether the network sends a GIF image or the short silent video Telegram sends (`fi.mau.gif`, `is_animated`); the interface loops it, muted, without controls.
  - `voice`: an `m.audio` with `org.matrix.msc3245.voice`, its duration from `info.duration` or `org.matrix.msc1767.audio`.
  - `MessageReply.kind` tells that an answered message is a photo, which may have no text to quote.
  - `media.read` (`media_id`, `offset`, `length` ≤ 1 MiB) → `mime_type`, `size`, `data` (base64). Media IDs are opaque (`f.` and the encoded content URI, like the other IDs, ADR 0012).
- **Core** (`core/embedded/media.go`): media without a stored file (encrypted media, which the local server never holds, or media a connector could not copy) stays `unsupported` with its text, as before. An edit that only changes a caption keeps the original's media.
- **Local server** (`core/localmatrix`):
  - `ReadMedia` reads a range with `substr` on the BLOB, never the whole media for a part;
  - a redaction deletes the media that the message and its edits name (file and thumbnail) before erasing their content: a deleted photo does not stay on the device, as T2.18 decided for text (ADR 0034);
  - schema v5 records the room each media was uploaded for (`local_media.room_id`, `ON DELETE CASCADE`), so that deleting a room (logging out an account) deletes its media; the upgrade gives older media the room of the first event that names it.
- **Matrix accounts** (`core/connector/matrix/media.go`): a received message's media is downloaded from the homeserver (authenticated media), decrypted when encrypted (`DecryptInPlace` checks its hash first), and uploaded to the local server, up to 100 MiB; thumbnails are not copied. Media that cannot be copied leaves the message without media; the warning logs the message type only, no URL nor file name.
- **Echo network**: `/image`, `/gif`, `/file` and `/voice` make the contact send a generated PNG (96 × 64), animated GIF, text file or WAV voice message (0.5 s), with the rest of the text as the caption.
- **Interface** (`ui/src/media.ts`, `ui/src/app/MessageMedia.tsx`):
  - photos and GIFs in the bubble, `alt` the caption or "Photo"/"GIF"; videos with controls; audio and voice messages with the player and their duration ("Voice message, 0:04" for screen readers); files with their name, size and a Save button;
  - photos and videos reserve their shape (`aspect-ratio` from `width` and `height`) before they are read, so that the virtualized thread does not jump;
  - the inbox's preview, quotes, the announcement of a new message and Android's notifications say what the media is: "Photo: a sunset", "Voice message", "File: plan.pdf".

## Consequences

- Telegram and Signal send their media through bridgev2's `UploadMedia`: **assumed** to show the same way, not run against the networks in T2.20. Telegram's stickers are `m.sticker` events, which the local server does not list as messages yet: they still do not show (left for later).
- Media is stored in clear in `core.db`, like message text (ADR 0019 seals sessions only). A whole-database encryption would cover both.
- The cache keeps up to 64 MB of Blobs in the page's memory, plus what the screen shows; on a low-end phone this may need lowering (to measure).
- Base64 over the transports costs a third more bytes; a 50 MB video takes 50 calls. If this is too slow on Android, a later ADR can add a binary path (for example a file the shell serves) without changing `Message.media`.
- Saving a file uses a download link: on the desktop, Electron's save dialog. **Unknown**: Android's WebView ignores downloads of `blob:` URLs unless the shell handles them; saving files on Android needs the shell (a Capacitor file or share plugin), left for later.
- A media that the network gave the wrong type is offered as a file: the user can still save it.
- Sending media is T2.21; recording voice messages is T2.22.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| Media is read in ranges, with its type, name and size; another server's media is not found | **implemented, unverified** | `core/localmatrix/media_test.go` `TestReadMediaInParts` |
| A redaction deletes the media of the message and of its edits; deleting a room deletes its media, not media of no room | **implemented, unverified** | `TestRedactionDeletesMedia`, `TestDeletingARoomDeletesItsMedia` |
| The schema v4 → v5 upgrade gives older media its room | **implemented, unverified** | no test yet: to add with the upgrade tests (`relations_test.go`) |
| The core gives photos, GIFs, files and voice messages their kind, caption and media, reads them back in parts, quotes an answered photo by its kind, refuses bad ranges and IDs, and forgets a deleted photo's bytes | **implemented, unverified** | `core/embedded/media_test.go` `TestReceiveMedia`, `TestMessageContent` |
| The contract: the new command and fields have examples, valid for Go's and TypeScript's validators | **implemented, unverified** | `core/api` `TestExamplesCoverTheSchema`; `ui/src/core-api/contract.test.ts` |
| Summaries, the allowed types (no SVG nor HTML), sizes, durations, base64, reading in parts and the cache | **implemented, unverified** | `ui/src/media.test.ts` |
| The desktop app shows a photo, a GIF and a voice message, the inbox says "Voice message", and a file is saved with its content | **implemented, unverified**; **unknown** whether Playwright sees Electron's download without a save dialog | `apps/desktop/e2e/app.spec.ts`, "the thread shows photos, GIFs and voice messages, and saves files" |
| A Matrix account copies bob's encrypted photo to the device, decrypted, and media.read gives it back | **implemented, unverified** (CI only: Synapse) | `infra/matrixtest` `TestMatrixAccount` (`receivedMedia`); `core/connector/matrix` `TestIncoming` |
| Android's notifications say what the media is | **implemented, unverified** | `MessageNotificationsTest.saysWhatMediaIs` |
| Telegram and Signal media show | **assumed** | |
