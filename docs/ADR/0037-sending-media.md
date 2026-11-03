# ADR 0037 — Sending media: uploads in parts kept in the core's memory, kinds from the network's room features

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.21
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

T2.20 (ADR 0036) shows received media. The user could still only send text. Sending media raises the mirror of ADR 0036's question: the interface holds the bytes (a `File` from a file picker or the clipboard), the core must store them in the local server and hand the message to the bridge, and the JSON API is the only channel (ADR 0012).

bridgev2 checks a media message against the network's room features before calling the connector (`Portal.checkMessageContentCaps`): `RoomFeatures.File` maps a capability message type (`m.image`, `m.video`, `m.audio`, `org.matrix.msc3245.voice`, `m.file`, …) to `FileFeatures` (MIME types, caption, maximum size and duration). A message whose type is not in the map is refused with `ErrUnsupportedMessageType`.

Sources: mautrix-go v0.31.0 `bridgev2/portal.go` (`checkMessageContentCaps`, `handleMatrixMessage`), `event/capabilities.go` (`FileFeatures`, `FileFeatureMap`, `GetMimeSupport`, `CapMsgVoice`), `event/message.go` (`GetCapMsgType`), `crypto/attachment/attachments.go` (`NewEncryptedFile`, `EncryptInPlace`), `client.go` (`UploadBytes`); the Matrix client-server specification, "Sending encrypted attachments".

## Options considered

### How the bytes reach the core

#### Option A — `uploads.append` in parts, then `messages.sendMedia` (chosen)
The interface gives the file in parts of at most 1 MiB in base64 (`uploads.append`, which returns an upload ID and the size so far), then sends the upload with its description (`messages.sendMedia`: kind, MIME type, file name, caption, size, duration, reply).
- Pros: the same transport as everything else; progress for free; a refused send keeps the upload to try again.
- Cons: base64 and one call per MiB, as for reading (ADR 0036).

#### Option B — One `messages.sendMedia` with all the bytes
- Cons: a 50 MB video in one JSON document, through JNI or a pipe; no progress.

### Where the parts wait

#### Option C — In the core's memory (chosen)
- Pros: the media never lands on the disk in clear outside the database; nothing to clean up after a crash.
- Cons: memory: an upload holds at most 100 MiB, all uploads together 200 MiB; an upload left alone for 10 minutes is dropped. On a phone with little memory a 100 MiB video is a lot (to measure).

#### Option D — A temporary file in the data directory
- Cons: plaintext media on disk until sent, to delete after a crash.

### Which kinds the interface offers

#### Option E — `Conversation.actions.media`, from the room features (chosen)
Each kind (`image`, `video`, `audio`, `voice`, `file`) is listed when `RoomFeatures.File` has it, with `max_size` and whether it takes a caption. The interface sends a photo of an inline type as an image, a video as a video, audio as audio, and anything else, or a kind the network does not take, as a file. The core checks the kind, the caption and the size again before using the upload.

## Decision

**Options A, C and E.**

- **API 1.10**: `uploads.append`, `uploads.cancel`, `messages.sendMedia` (answering like `messages.send`: the message at once with status `sending`, its delivery later by `message.updated`), `ConversationActions.media` with `MediaActions` and `MediaAction`.
- **Core** (`core/embedded/upload.go`): the limits above; a file name must be a plain name (no `/`, `\` nor NUL); a voice message is an `m.audio` with `org.matrix.msc3245.voice` and `org.matrix.msc1767.audio`; the body is the caption, or the file name without one (the Matrix rule that `body` is a caption when it differs from `filename`). `Server.UploadMedia` stores the bytes with the room (schema v5, ADR 0036).
- **Echo network**: takes every kind up to 10 MiB with a caption, and sends back its own copy of the media, so that deleting one message never deletes the other's media.
- **Matrix accounts**: every kind up to 50 MiB (Synapse's default `max_upload_size`; a homeserver that takes less refuses the upload, and the message fails with its answer); the connector reads the media on the device, encrypts it when the room is encrypted (an encrypted attachment named in `file`, uploaded as `application/octet-stream`), and uploads it with the user's access token. Thumbnails are not made.
- **Interface**: an "Attach a file" button (📎) and pasting a file attach it; the attached file shows above the text, which becomes its caption, with "Remove"; sending shows the progress; a problem the network's limits make known (too large, a caption where none is taken) is said before anything is read; a failed send cancels its upload and keeps the attached file.

## Consequences

- Telegram and Signal take media through their connectors' `HandleMatrixMessage`, which download it from the local server (`DownloadMedia`): **assumed**, not run in T2.21. Their room features say which kinds and sizes they take.
- The interface sends no thumbnail, and the duration of videos and audio only when known (not measured yet): networks that need them may show less.
- Android: the file picker of the WebView works through Capacitor's `BridgeWebChromeClient` (**assumed**, not tried).
- A 100 MiB upload costs 100 MiB of the core's memory, then the database's copy: to measure on a low-end phone before raising the limits.
- Recording voice messages in the app is T2.22; this task sends voice messages only from a file.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The core sends a photo with a caption and a voice message in answer, in parts; the echo network sends back its own copy; the actions list every kind | **implemented, unverified** | `core/embedded/upload_test.go` `TestSendMedia` |
| The core refuses a text kind, a path as file name, an unknown or cancelled upload, a media over the network's limit (keeping the upload), a part over 1 MiB, bad base64 | **implemented, unverified** | `TestSendMediaRefusals` |
| An upload is bounded, all uploads together too, and an idle one goes | **implemented, unverified** | `TestUploadsLimits` |
| The contract: the new commands and fields have examples, valid for Go's and TypeScript's validators | **implemented, unverified** | `core/api` `TestExamplesCoverTheSchema`; `ui/src/core-api/contract.test.ts` |
| How a file is sent, why it cannot be, base64 and the parts | **implemented, unverified** | `ui/src/media.test.ts` ("sending") |
| The store uploads in parts and sends with the caption and the answer, and refuses a file over the network's limit before reading it | **implemented, unverified** | `ui/src/store.test.ts` |
| The desktop app sends a photo with a caption, shows it and its echo, removes and sends a file | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "the user sends a photo with a caption, and a file" |
| A Matrix account sends an encrypted file that bob decrypts; the homeserver holds only ciphertext | **implemented, unverified** (CI only: Synapse) | `infra/matrixtest` `TestMatrixAccount` (`sentMedia`) |
| Telegram and Signal take the media | **assumed** | |
