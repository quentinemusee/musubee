// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"testing"

	"github.com/quentinemusee/musubee/core/api"
)

// TestStarred stars a message, lists it, and unstars it (API 1.36,
// docs/ADR/0065).
func TestStarred(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Instant Echo")
	sent := call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: "keep this"}).Message
	star := func(on bool) api.Message {
		t.Helper()
		return call[api.MessagesSendResult](t, c, api.CommandMessagesStar, api.MessagesStarParams{MessageID: sent.MessageID, Starred: on}).Message
	}
	listed := func() []api.Message {
		t.Helper()
		return call[api.MessagesStarredResult](t, c, api.CommandMessagesStarred, nil).Messages
	}
	if msg := star(true); !msg.Starred || msg.Text != "keep this" {
		t.Errorf("starred message = %+v", msg)
	}
	if got := listed(); len(got) != 1 || got[0].MessageID != sent.MessageID || !got[0].Starred {
		t.Errorf("starred list = %+v", got)
	}
	// The conversation's messages say it too.
	page := call[api.MessagesListResult](t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID}).Messages
	found := false
	for _, msg := range page {
		if msg.MessageID == sent.MessageID {
			found = msg.Starred
		}
	}
	if !found {
		t.Error("messages.list does not say the message is starred")
	}
	if msg := star(false); msg.Starred {
		t.Errorf("unstarred message = %+v", msg)
	}
	if got := listed(); len(got) != 0 {
		t.Errorf("starred list after unstarring = %+v", got)
	}
	callFails(t, c, api.CommandMessagesStar, api.MessagesStarParams{MessageID: "m.nope", Starred: true}, api.ErrorCodeNotFound)
}
