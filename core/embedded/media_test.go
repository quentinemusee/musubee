// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"testing"

	"maunium.net/go/mautrix/event"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/connector/echo"
)

// readAll reads a media through media.read, in parts of size bytes.
func readAll(t *testing.T, c *Core, mediaID string, size int64) (string, []byte) {
	t.Helper()
	var data []byte
	var mimeType string
	for {
		part := call[api.MediaReadResult](t, c, api.CommandMediaRead, api.MediaReadParams{MediaID: mediaID, Offset: int64(len(data)), Length: size})
		chunk, err := base64.StdEncoding.DecodeString(part.Data)
		if err != nil {
			t.Fatal(err)
		}
		mimeType = part.MimeType
		data = append(data, chunk...)
		if len(chunk) == 0 || int64(len(data)) >= part.Size {
			return mimeType, data
		}
	}
}

// TestReceiveMedia receives a photo, a voice message and a file from the
// echo network's media commands (API 1.9), reads their bytes in parts,
// then deletes the photo, which takes its bytes off the device.
func TestReceiveMedia(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Instant Echo")
	receive := func(text string) api.Message {
		t.Helper()
		call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: text})
		e := waitEvent(t, c, text, messageEvent(t, api.EventMessageAdded, func(m api.Message) bool { return !m.FromMe && m.Media != nil }))
		return decode[api.MessageEvent](t, e.Data).Message
	}

	photo := receive(echo.CommandImage + " a sunset")
	if photo.Kind != api.MessageKindImage || photo.Text != "a sunset" || photo.Media.MimeType != "image/png" ||
		photo.Media.Width != echo.ImageWidth || photo.Media.Height != echo.ImageHeight || photo.Media.FileName != "echo.png" || photo.Media.Animated {
		t.Errorf("photo = %+v, media %+v", photo, photo.Media)
	}
	mimeType, data := readAll(t, c, photo.Media.MediaID, 100)
	if mimeType != "image/png" || int64(len(data)) != photo.Media.Size {
		t.Errorf("read %d bytes of %s, want %d of image/png", len(data), mimeType, photo.Media.Size)
	}
	if img, err := png.Decode(bytes.NewReader(data)); err != nil || img.Bounds().Dx() != echo.ImageWidth {
		t.Errorf("the photo read back is not the PNG sent: %v", err)
	}

	voice := receive(echo.CommandVoice)
	if voice.Kind != api.MessageKindVoice || voice.Text != "" || voice.Media.DurationMS != echo.VoiceDurationMS {
		t.Errorf("voice = %+v, media %+v", voice, voice.Media)
	}
	animation := receive(echo.CommandGIF)
	if animation.Kind != api.MessageKindImage || !animation.Media.Animated {
		t.Errorf("GIF = %+v, media %+v", animation, animation.Media)
	}
	file := receive(echo.CommandFile)
	if file.Kind != api.MessageKindFile || file.Media.FileName != "echo.txt" || file.Text != "" {
		t.Errorf("file = %+v, media %+v", file, file.Media)
	}
	if _, data = readAll(t, c, file.Media.MediaID, 1<<20); string(data) != echo.FileText {
		t.Errorf("file content = %q, want %q", data, echo.FileText)
	}

	// The answer to a photo says it is one.
	call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: "nice", ReplyTo: photo.MessageID})
	e := waitEvent(t, c, "the answer", messageEvent(t, api.EventMessageAdded, func(m api.Message) bool { return m.FromMe && m.ReplyTo != nil }))
	if reply := decode[api.MessageEvent](t, e.Data).Message.ReplyTo; reply.Kind != api.MessageKindImage || reply.Text != "a sunset" {
		t.Errorf("reply_to = %+v, want the photo's kind and caption", reply)
	}

	// Reading past the end gives nothing; bad ranges and IDs are refused.
	past := call[api.MediaReadResult](t, c, api.CommandMediaRead, api.MediaReadParams{MediaID: file.Media.MediaID, Offset: 1 << 20})
	if past.Data != "" || past.Size != int64(len(echo.FileText)) {
		t.Errorf("read past the end = %+v", past)
	}
	callFails(t, c, api.CommandMediaRead, api.MediaReadParams{MediaID: file.Media.MediaID, Length: 1<<20 + 1}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandMediaRead, api.MediaReadParams{MediaID: photo.MessageID}, api.ErrorCodeNotFound)

	// Deleting the photo on the network deletes its bytes too.
	roomID, eventID, err := parseMessageID(photo.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.host.Matrix.SendEvent(t.Context(), roomID, event.EventRedaction, &event.RedactionEventContent{Redacts: eventID}); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, c, "the deleted photo", messageEvent(t, api.EventMessageUpdated, func(m api.Message) bool {
		return m.MessageID == photo.MessageID && m.Deleted && m.Media == nil
	}))
	callFails(t, c, api.CommandMediaRead, api.MediaReadParams{MediaID: photo.Media.MediaID}, api.ErrorCodeNotFound)
}

func TestMessageContent(t *testing.T) {
	image := &event.MessageEventContent{
		MsgType: event.MsgImage, Body: "photo.jpg", URL: "mxc://local/a",
		Info: &event.FileInfo{MimeType: "image/jpeg", Width: 4, Height: 3, Size: 12, ThumbnailURL: "mxc://local/b"},
	}
	kind, text, media := messageContent(image, image)
	if kind != api.MessageKindImage || text != "" || media.FileName != "photo.jpg" || media.ThumbnailID != mediaID("mxc://local/b") || media.Size != 12 {
		t.Errorf("image: %s %q %+v", kind, text, media)
	}
	// An edit of the caption alone keeps the media.
	caption := &event.MessageEventContent{MsgType: event.MsgImage, Body: "new caption"}
	if kind, text, media = messageContent(image, caption); kind != api.MessageKindImage || text != "new caption" || media == nil || media.MediaID != mediaID("mxc://local/a") {
		t.Errorf("caption edit: %s %q %+v", kind, text, media)
	}
	// Telegram's GIFs are silent videos.
	gif := &event.MessageEventContent{MsgType: event.MsgVideo, Body: "x.mp4", URL: "mxc://local/c", Info: &event.FileInfo{MimeType: "video/mp4", MauGIF: true, Duration: 900}}
	if kind, _, media = messageContent(gif, gif); kind != api.MessageKindVideo || !media.Animated || media.DurationMS != 900 {
		t.Errorf("GIF: %s %+v", kind, media)
	}
	// Encrypted media, never stored on the device, falls back to its text.
	encrypted := &event.MessageEventContent{MsgType: event.MsgFile, Body: "secret.pdf", File: &event.EncryptedFileInfo{}}
	if kind, text, media = messageContent(encrypted, encrypted); kind != api.MessageKindUnsupported || text != "secret.pdf" || media != nil {
		t.Errorf("encrypted: %s %q %+v", kind, text, media)
	}
}
