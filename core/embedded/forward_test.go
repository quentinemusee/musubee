// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"bytes"
	"testing"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/connector/echo"
)

// TestForward copies a text, a photo with its caption and a GIF from one
// conversation to another (API 1.26, docs/ADR/0054).
func TestForward(t *testing.T) {
	c := open(t)
	login(t, c)
	from := conversationNamed(t, c, "Instant Echo")
	to := conversationNamed(t, c, "Delayed Echo")
	receive := func(text string) api.Message {
		t.Helper()
		call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: from.ConversationID, Text: text})
		e := waitEvent(t, c, text, messageEvent(t, api.EventMessageAdded, func(m api.Message) bool { return !m.FromMe && m.Media != nil }))
		return decode[api.MessageEvent](t, e.Data).Message
	}
	forward := func(message string) api.Message {
		t.Helper()
		return call[api.MessagesSendResult](t, c, api.CommandMessagesForward, api.MessagesForwardParams{MessageID: message, ConversationID: to.ConversationID}).Message
	}

	text := call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: from.ConversationID, Text: "worth sharing"}).Message
	if copied := forward(text.MessageID); copied.ConversationID != to.ConversationID || !copied.FromMe || copied.Text != "worth sharing" || copied.ReplyTo != nil {
		t.Errorf("forwarded text = %+v", copied)
	}

	photo := receive(echo.CommandImage + " a sunset")
	copied := forward(photo.MessageID)
	if copied.Kind != api.MessageKindImage || copied.Text != "a sunset" || copied.Media == nil || copied.Media.Size != photo.Media.Size ||
		copied.Media.Width != photo.Media.Width || copied.Media.MediaID == photo.Media.MediaID {
		t.Fatalf("forwarded photo = %+v, media %+v", copied, copied.Media)
	}
	_, original := readAll(t, c, photo.Media.MediaID, 100)
	if _, data := readAll(t, c, copied.Media.MediaID, 100); !bytes.Equal(data, original) {
		t.Errorf("the forwarded photo's bytes differ from the original's")
	}

	if animation := forward(receive(echo.CommandGIF).MessageID); animation.Media == nil || !animation.Media.Animated {
		t.Errorf("a forwarded GIF is no longer animated: %+v", animation.Media)
	}

	callFails(t, c, api.CommandMessagesForward, api.MessagesForwardParams{MessageID: "m.nope", ConversationID: to.ConversationID}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandMessagesForward, api.MessagesForwardParams{MessageID: text.MessageID, ConversationID: "c.nope"}, api.ErrorCodeNotFound)
}
