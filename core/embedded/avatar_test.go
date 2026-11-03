// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/connector/echo"
)

// TestAvatar: a direct conversation shows its echo contact's picture,
// which media.read gives back (API 1.32, docs/ADR/0061).
func TestAvatar(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Instant Echo")
	if conv.AvatarID == "" {
		// The picture may come after the conversation.
		conv = waitConversationUpdate(t, c, "avatar", conv.ConversationID, func(cv api.Conversation) bool { return cv.AvatarID != "" })
	}
	mimeType, data := readAll(t, c, conv.AvatarID, 100)
	if mimeType != "image/png" {
		t.Errorf("avatar type = %q, want image/png", mimeType)
	}
	if img, err := png.Decode(bytes.NewReader(data)); err != nil || img.Bounds().Dx() != echo.ImageWidth {
		t.Errorf("the avatar is not the contact's picture: %v", err)
	}
}

// TestSenderAvatar: the echo contact's answer carries its picture; the
// user's own message does not (API 1.33).
func TestSenderAvatar(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Instant Echo")
	sent := call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: "smile"}).Message
	if sent.SenderAvatarID != "" {
		t.Errorf("the user's own message has a sender picture: %q", sent.SenderAvatarID)
	}
	e := waitEvent(t, c, "the echo", messageEvent(t, api.EventMessageAdded, func(m api.Message) bool { return !m.FromMe && m.Text == "smile" }))
	answer := decode[api.MessageEvent](t, e.Data).Message
	if answer.SenderAvatarID == "" {
		t.Fatalf("the echo has no sender picture: %+v", answer)
	}
	if mimeType, _ := readAll(t, c, answer.SenderAvatarID, 100); mimeType != "image/png" {
		t.Errorf("sender picture type = %q", mimeType)
	}
}
