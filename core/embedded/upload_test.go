// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"bytes"
	"encoding/base64"
	"slices"
	"testing"
	"time"

	"maunium.net/go/mautrix/event"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/connector/echo"
)

// upload gives data to the core in parts of size bytes, and returns the
// upload's ID.
func uploadBytes(t *testing.T, c *Core, data []byte, size int) string {
	t.Helper()
	uploadID := ""
	for start := 0; start < len(data); start += size {
		part := data[start:min(start+size, len(data))]
		result := call[api.UploadsAppendResult](t, c, api.CommandUploadsAppend, api.UploadsAppendParams{UploadID: uploadID, Data: base64.StdEncoding.EncodeToString(part)})
		uploadID = result.UploadID
		if result.Size != int64(start+len(part)) {
			t.Fatalf("upload size = %d, want %d", result.Size, start+len(part))
		}
	}
	return uploadID
}

// TestSendMedia sends a photo with a caption and a voice message to the
// echo network (API 1.10), which sends each back as its own copy.
func TestSendMedia(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Instant Echo")
	media := conv.Actions.Media
	if media == nil || media.Image == nil || !media.Image.Caption || media.Image.MaxSize != echo.MaxMediaSize || media.Voice == nil || media.File == nil {
		t.Fatalf("media actions = %+v, want every kind with a caption", media)
	}

	photo := bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 1000)
	sent := call[api.MessagesSendResult](t, c, api.CommandMessagesSendMedia, api.MessagesSendMediaParams{
		ConversationID: conv.ConversationID, UploadID: uploadBytes(t, c, photo, 1500),
		Kind: api.MessageKindImage, MimeType: "image/png", FileName: "photo.png", Caption: "my photo", Width: 40, Height: 30,
	}).Message
	if !sent.FromMe || sent.Kind != api.MessageKindImage || sent.Text != "my photo" || sent.Status != api.MessageStatusSending ||
		sent.Media == nil || sent.Media.Size != int64(len(photo)) || sent.Media.Width != 40 || sent.Media.FileName != "photo.png" {
		t.Fatalf("sent = %+v, media %+v", sent, sent.Media)
	}
	if _, data := readAll(t, c, sent.Media.MediaID, 1<<20); !bytes.Equal(data, photo) {
		t.Errorf("the sent photo reads back as %d bytes, want %d", len(data), len(photo))
	}
	waitEvent(t, c, "the photo sent", messageEvent(t, api.EventMessageUpdated, func(m api.Message) bool {
		return m.MessageID == sent.MessageID && m.Status == api.MessageStatusSent
	}))
	e := waitEvent(t, c, "the photo echoed", messageEvent(t, api.EventMessageAdded, func(m api.Message) bool { return !m.FromMe && m.Kind == api.MessageKindImage }))
	echoed := decode[api.MessageEvent](t, e.Data).Message
	if echoed.Text != "my photo" || echoed.Media.MediaID == sent.Media.MediaID {
		t.Errorf("echo = %+v, media %+v; want the caption and a copy of the media", echoed, echoed.Media)
	}
	if _, data := readAll(t, c, echoed.Media.MediaID, 1<<20); !bytes.Equal(data, photo) {
		t.Errorf("the echoed photo has %d bytes, want %d", len(data), len(photo))
	}

	voice := call[api.MessagesSendResult](t, c, api.CommandMessagesSendMedia, api.MessagesSendMediaParams{
		ConversationID: conv.ConversationID, UploadID: uploadBytes(t, c, []byte("RIFF voice"), 1<<20),
		Kind: api.MessageKindVoice, MimeType: "audio/ogg", FileName: "voice.ogg", DurationMS: 3200, ReplyTo: echoed.MessageID,
	}).Message
	if voice.Kind != api.MessageKindVoice || voice.Media.DurationMS != 3200 || voice.Text != "" || voice.ReplyTo == nil || voice.ReplyTo.Kind != api.MessageKindImage {
		t.Errorf("voice = %+v, media %+v", voice, voice.Media)
	}
}

func TestSendMediaRefusals(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Instant Echo")
	send := func(p api.MessagesSendMediaParams, code api.ErrorCode) {
		t.Helper()
		if p.ConversationID == "" {
			p.ConversationID = conv.ConversationID
		}
		if p.FileName == "" {
			p.FileName = "a.bin"
		}
		callFails(t, c, api.CommandMessagesSendMedia, p, code)
	}
	small := uploadBytes(t, c, []byte("abc"), 10)
	send(api.MessagesSendMediaParams{UploadID: small, Kind: api.MessageKindText}, api.ErrorCodeInvalidParams)
	send(api.MessagesSendMediaParams{UploadID: small, Kind: api.MessageKindFile, FileName: "../x"}, api.ErrorCodeInvalidParams)
	send(api.MessagesSendMediaParams{UploadID: "u.unknown", Kind: api.MessageKindFile}, api.ErrorCodeNotFound)
	// The network's limit (10 MiB) is checked before the upload is used.
	large := uploadBytes(t, c, make([]byte, echo.MaxMediaSize+1), 1<<20)
	send(api.MessagesSendMediaParams{UploadID: large, Kind: api.MessageKindFile}, api.ErrorCodeInvalidParams)
	call[api.Empty](t, c, api.CommandUploadsCancel, api.UploadsCancelParams{UploadID: large})
	send(api.MessagesSendMediaParams{UploadID: large, Kind: api.MessageKindFile}, api.ErrorCodeNotFound)
	// A part over 1 MiB, bad base64, an unknown upload.
	callFails(t, c, api.CommandUploadsAppend, api.UploadsAppendParams{Data: base64.StdEncoding.EncodeToString(make([]byte, maxMediaRead+1))}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandUploadsAppend, api.UploadsAppendParams{Data: "not base64!"}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandUploadsAppend, api.UploadsAppendParams{UploadID: "u.unknown", Data: "AA=="}, api.ErrorCodeNotFound)
	// The small upload was never used: it still sends.
	call[api.MessagesSendResult](t, c, api.CommandMessagesSendMedia, api.MessagesSendMediaParams{
		ConversationID: conv.ConversationID, UploadID: small, Kind: api.MessageKindFile, FileName: "abc.txt", MimeType: "text/plain",
	})
}

func TestUploadsLimits(t *testing.T) {
	now := time.Unix(0, 0)
	u := uploads{maxBytes: 100, maxTotal: 200}
	u.nowFunc = func() time.Time { return now }
	first, _, err := u.append("", make([]byte, 100))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = u.append(first, []byte{1}); err == nil {
		t.Error("an upload grew past its limit")
	}
	if u.size(first) != 0 {
		t.Error("an upload past the limit is kept")
	}
	a, _, _ := u.append("", make([]byte, 100))
	b, _, _ := u.append("", make([]byte, 100))
	if _, _, err = u.append("", []byte{1}); err == nil {
		t.Error("uploads grew past their limit together")
	}
	// Left alone, they go.
	now = now.Add(uploadIdle + time.Second)
	if _, _, err = u.append("", []byte{1}); err != nil {
		t.Errorf("after the idle uploads went: %v", err)
	}
	if u.size(a) != 0 || u.size(b) != 0 {
		t.Error("idle uploads are kept")
	}
}

func TestMimeTypes(t *testing.T) {
	full, unsupported := event.CapLevelFullySupported, event.CapLevelUnsupported
	for _, tc := range []struct {
		types map[string]event.CapabilitySupportLevel
		want  []string
		ok    bool
	}{
		{map[string]event.CapabilitySupportLevel{"*/*": full}, nil, true},
		// Signal's voice messages without ffmpeg: AAC only.
		{map[string]event.CapabilitySupportLevel{"audio/aac": full, "audio/ogg": unsupported}, []string{"audio/aac"}, true},
		{map[string]event.CapabilitySupportLevel{"audio/ogg": full, "audio/mp4": event.CapLevelPartialSupport}, []string{"audio/mp4", "audio/ogg"}, true},
		{map[string]event.CapabilitySupportLevel{"audio/ogg": unsupported}, nil, false},
		{nil, nil, false},
	} {
		got, ok := mimeTypes(&event.FileFeatures{MimeTypes: tc.types})
		if ok != tc.ok || !slices.Equal(got, tc.want) {
			t.Errorf("mimeTypes(%v) = %v, %v; want %v, %v", tc.types, got, ok, tc.want, tc.ok)
		}
	}
}
