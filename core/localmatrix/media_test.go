// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package localmatrix_test

import (
	"bytes"
	"errors"
	"testing"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/localmatrix"
)

func TestReadMediaInParts(t *testing.T) {
	server := newServer(t, localmatrix.Options{UserLocalpart: "me"})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	roomID := createDM(t, server, conn, "alice")
	alice := conn.GhostIntent("alice")

	data := bytes.Repeat([]byte("0123456789"), 10)
	uri, _, err := alice.UploadMedia(ctx, roomID, data, "digits.txt", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		offset, length int64
		want           []byte
	}{
		{0, 10, data[:10]},
		{95, 10, data[95:]},
		{100, 10, nil},
		{0, 1000, data},
	} {
		part, err := server.ReadMedia(ctx, uri, tc.offset, tc.length)
		if err != nil {
			t.Fatal(err)
		}
		if part.Size != 100 || part.MimeType != "text/plain" || part.FileName != "digits.txt" {
			t.Errorf("part = %+v, want the size, type and name of the media", part)
		}
		if !bytes.Equal(part.Data, tc.want) {
			t.Errorf("read %d bytes at %d = %q, want %q", tc.length, tc.offset, part.Data, tc.want)
		}
	}
	other := id.ContentURI{Homeserver: "elsewhere.example", FileID: "x"}.CUString()
	if _, err = server.ReadMedia(ctx, other, 0, 1); !errors.Is(err, localmatrix.ErrNotFound) {
		t.Errorf("media of another server: err = %v, want ErrNotFound", err)
	}
}

func image(uri, thumbnail id.ContentURIString, caption string) *event.MessageEventContent {
	return &event.MessageEventContent{
		MsgType:  event.MsgImage,
		Body:     caption,
		FileName: "photo.png",
		URL:      uri,
		Info:     &event.FileInfo{MimeType: "image/png", ThumbnailURL: thumbnail},
	}
}

func TestRedactionDeletesMedia(t *testing.T) {
	server := newServer(t, localmatrix.Options{UserLocalpart: "me"})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	roomID := createDM(t, server, conn, "alice")
	alice := conn.GhostIntent("alice")

	upload := func(name string) id.ContentURIString {
		uri, _, err := alice.UploadMedia(ctx, roomID, []byte(name), name, "image/png")
		if err != nil {
			t.Fatal(err)
		}
		return uri
	}
	photo, thumbnail, replaced, kept := upload("photo"), upload("thumbnail"), upload("replaced"), upload("kept")
	msg := send(t, alice, roomID, event.EventMessage, image(photo, thumbnail, "a caption"))
	changed := image(replaced, "", "another caption")
	changed.SetEdit(msg)
	send(t, alice, roomID, event.EventMessage, changed)
	send(t, alice, roomID, event.EventMessage, image(kept, "", ""))
	send(t, alice, roomID, event.EventRedaction, redaction(msg))

	for _, uri := range []id.ContentURIString{photo, thumbnail, replaced} {
		if _, err := server.ReadMedia(ctx, uri, 0, 1); !errors.Is(err, localmatrix.ErrNotFound) {
			t.Errorf("%s after the redaction: err = %v, want ErrNotFound", uri, err)
		}
	}
	if _, err := server.ReadMedia(ctx, kept, 0, 1); err != nil {
		t.Errorf("the media of another message: %v", err)
	}
}

func TestDeletingARoomDeletesItsMedia(t *testing.T) {
	server := newServer(t, localmatrix.Options{UserLocalpart: "me"})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	roomID := createDM(t, server, conn, "alice")
	alice := conn.GhostIntent("alice")

	inRoom, _, err := alice.UploadMedia(ctx, roomID, []byte("in the room"), "a.png", "image/png")
	if err != nil {
		t.Fatal(err)
	}
	avatar, _, err := alice.UploadMedia(ctx, "", []byte("avatar"), "b.png", "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if err = conn.BotIntent().DeleteRoom(ctx, roomID, false); err != nil {
		t.Fatal(err)
	}
	if _, err = server.ReadMedia(ctx, inRoom, 0, 1); !errors.Is(err, localmatrix.ErrNotFound) {
		t.Errorf("media of the deleted room: err = %v, want ErrNotFound", err)
	}
	if _, err = server.ReadMedia(ctx, avatar, 0, 1); err != nil {
		t.Errorf("media of no room: %v", err)
	}
}
