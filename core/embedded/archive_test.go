// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"testing"

	"github.com/quentinemusee/musubee/core/api"
)

// TestArchive archives a conversation, which an answer brings back unless
// the conversation is muted (API 1.31, docs/ADR/0060).
func TestArchive(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Instant Echo")
	archive := func(archived bool) {
		t.Helper()
		call[api.Empty](t, c, api.CommandConversationsArchive, api.ConversationsArchiveParams{ConversationID: conv.ConversationID, Archived: archived})
	}
	send := func(text string) {
		t.Helper()
		call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: text})
		waitEvent(t, c, text, messageEvent(t, api.EventMessageAdded, func(m api.Message) bool { return !m.FromMe && m.Text == text }))
	}
	archived := func() bool {
		t.Helper()
		for _, other := range call[api.ConversationsListResult](t, c, api.CommandConversationsList, nil).Conversations {
			if other.ConversationID == conv.ConversationID {
				return other.Archived
			}
		}
		t.Fatal("the conversation is not listed")
		return false
	}

	archive(true)
	waitConversationUpdate(t, c, "archived", conv.ConversationID, func(cv api.Conversation) bool { return cv.Archived })
	// The echo's answer brings it back.
	send("are you there?")
	waitConversationUpdate(t, c, "back", conv.ConversationID, func(cv api.Conversation) bool { return !cv.Archived })

	// Muted, it stays archived.
	call[api.Empty](t, c, api.CommandConversationsMute, api.ConversationsMuteParams{ConversationID: conv.ConversationID, UntilMS: -1})
	archive(true)
	waitConversationUpdate(t, c, "archived again", conv.ConversationID, func(cv api.Conversation) bool { return cv.Archived })
	send("still there?")
	if !archived() {
		t.Error("a muted conversation came back from the archive")
	}
	archive(false)
	if archived() {
		t.Error("unarchived by hand, the conversation is still archived")
	}
	callFails(t, c, api.CommandConversationsArchive, api.ConversationsArchiveParams{ConversationID: "c.nope", Archived: true}, api.ErrorCodeNotFound)
}
