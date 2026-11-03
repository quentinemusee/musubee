// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"slices"
	"strings"
	"testing"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/connector/echo"
)

// TestMessageChanges follows what a network does to messages after sending
// them, with the echo network's commands: a reaction, an edit, a deletion
// and a reply each change a message (message.updated), never add one.
func TestMessageChanges(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Instant Echo")
	send := func(text string) api.Message {
		t.Helper()
		return call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: text}).Message
	}
	// waitChange waits for a message.updated event that matches, failing
	// on a message.added event that is an edit's fallback ("* text").
	waitChange := func(what string, match func(api.Message) bool) api.Message {
		t.Helper()
		e := waitEvent(t, c, what, func(e rawEvent) bool {
			if e.Type == api.EventMessageAdded {
				if msg := decode[api.MessageEvent](t, e.Data).Message; strings.HasPrefix(msg.Text, "* ") {
					t.Errorf("an edit came as a new message: %+v", msg)
				}
			}
			return e.Type == api.EventMessageUpdated && match(decode[api.MessageEvent](t, e.Data).Message)
		})
		return decode[api.MessageEvent](t, e.Data).Message
	}

	reacted := send(echo.CommandReact + " 🎉")
	msg := waitChange("the reaction", func(m api.Message) bool { return m.MessageID == reacted.MessageID && len(m.Reactions) > 0 })
	if want := []api.MessageReaction{{Key: "🎉", Count: 1}}; !slices.Equal(msg.Reactions, want) {
		t.Errorf("reactions = %+v, want %+v", msg.Reactions, want)
	}

	send(echo.CommandEdit + " first try")
	msg = waitChange("the edit", func(m api.Message) bool { return m.Edited })
	if msg.FromMe || msg.Text != "first try"+echo.EditedSuffix || msg.Kind != api.MessageKindText {
		t.Errorf("edited echo = %+v", msg)
	}
	edited := msg.MessageID
	// The edited echo is the conversation's last message.
	waitConversationUpdate(t, c, "the conversation with the edited echo", conv.ConversationID, func(cv api.Conversation) bool {
		return cv.LastMessage != nil && cv.LastMessage.MessageID == edited && cv.LastMessage.Edited
	})

	send(echo.CommandDelete + " a secret")
	msg = waitChange("the deletion", func(m api.Message) bool { return m.Deleted })
	if msg.FromMe || msg.Text != "" || msg.Edited || msg.Reactions != nil {
		t.Errorf("deleted echo = %+v, want no text", msg)
	}
	deleted := msg.MessageID
	// A deleted message is not unread: the conversation says so at once.
	waitConversationUpdate(t, c, "the conversation without the deleted echo", conv.ConversationID, func(cv api.Conversation) bool {
		return cv.LastMessage != nil && cv.LastMessage.MessageID == deleted && cv.LastMessage.Deleted
	})

	question := send(echo.CommandReply + " an answer")
	e := waitEvent(t, c, "the answer", messageEvent(t, api.EventMessageAdded, func(m api.Message) bool { return !m.FromMe && m.ReplyTo != nil }))
	answer := decode[api.MessageEvent](t, e.Data).Message
	want := api.MessageReply{MessageID: question.MessageID, FromMe: true, SenderName: question.SenderName, Text: question.Text}
	if answer.Text != "an answer" || *answer.ReplyTo != want {
		t.Errorf("answer = %+v, reply_to %+v; want %+v", answer, *answer.ReplyTo, want)
	}

	// The history has each message once, as changed.
	list := call[api.MessagesListResult](t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID})
	var texts []string
	for _, m := range list.Messages {
		texts = append(texts, m.Text)
	}
	wantTexts := []string{
		"/react 🎉",
		"/edit first try", "first try (edited)",
		"/delete a secret", "",
		"/reply an answer", "an answer",
	}
	if !slices.Equal(texts, wantTexts) {
		t.Fatalf("history = %q, want %q", texts, wantTexts)
	}
	if got := list.Messages[0].Reactions; len(got) != 1 || got[0].Key != "🎉" {
		t.Errorf("listed reactions = %+v", got)
	}
	if !list.Messages[2].Edited || !list.Messages[4].Deleted || list.Messages[6].ReplyTo == nil {
		t.Errorf("listed changes lost: %+v", list.Messages)
	}
	// Unread: the answer, after the user's own last message.
	if cv := conversationNamed(t, c, "Instant Echo"); cv.UnreadCount != 1 {
		t.Errorf("unread = %d, want 1", cv.UnreadCount)
	}
}

// TestUserChanges makes the user's own changes through the API (ADR 0035):
// a reaction, taken back, an edit, a reply and a deletion, each waiting for
// the network's answer; and what the commands refuse.
func TestUserChanges(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Instant Echo")
	if want := (api.ConversationActions{Reply: true, React: true, Edit: true, Delete: true}); conv.Actions == nil || *conv.Actions != want {
		t.Fatalf("actions = %+v, want %+v", conv.Actions, want)
	}
	sent := call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: "hello"}).Message
	e := waitEvent(t, c, "the echo", messageEvent(t, api.EventMessageAdded, func(m api.Message) bool { return !m.FromMe }))
	echoed := decode[api.MessageEvent](t, e.Data).Message
	updated := func(what, messageID string, match func(api.Message) bool) api.Message {
		t.Helper()
		e := waitEvent(t, c, what, messageEvent(t, api.EventMessageUpdated, func(m api.Message) bool { return m.MessageID == messageID && match(m) }))
		return decode[api.MessageEvent](t, e.Data).Message
	}
	reactParams := api.MessagesReactParams{ConversationID: conv.ConversationID, MessageID: echoed.MessageID, Key: "👍"}

	call[api.Empty](t, c, api.CommandMessagesReact, reactParams)
	msg := updated("the reaction", echoed.MessageID, func(m api.Message) bool { return len(m.Reactions) > 0 })
	if want := []api.MessageReaction{{Key: "👍️", Count: 1, FromMe: true}}; !slices.Equal(msg.Reactions, want) {
		t.Errorf("reactions = %+v, want %+v", msg.Reactions, want)
	}
	// The same reaction again changes nothing.
	call[api.Empty](t, c, api.CommandMessagesReact, reactParams)
	call[api.Empty](t, c, api.CommandMessagesUnreact, reactParams)
	updated("the reaction taken back", echoed.MessageID, func(m api.Message) bool { return len(m.Reactions) == 0 })
	// Without the reaction, nothing to take back.
	call[api.Empty](t, c, api.CommandMessagesUnreact, reactParams)

	call[api.Empty](t, c, api.CommandMessagesEdit, api.MessagesEditParams{ConversationID: conv.ConversationID, MessageID: sent.MessageID, Text: "hello again"})
	msg = updated("the edit", sent.MessageID, func(m api.Message) bool { return m.Edited })
	if msg.Text != "hello again" || !msg.FromMe {
		t.Errorf("edited message = %+v", msg)
	}

	answer := call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: "an answer", ReplyTo: echoed.MessageID}).Message
	if want := (api.MessageReply{MessageID: echoed.MessageID, SenderName: echoed.SenderName, Text: echoed.Text}); answer.ReplyTo == nil || *answer.ReplyTo != want {
		t.Errorf("reply_to = %+v, want %+v", answer.ReplyTo, want)
	}
	updated("the answer sent", answer.MessageID, func(m api.Message) bool { return m.Status == api.MessageStatusSent })

	call[api.Empty](t, c, api.CommandMessagesDelete, api.MessagesDeleteParams{ConversationID: conv.ConversationID, MessageID: sent.MessageID})
	msg = updated("the deletion", sent.MessageID, func(m api.Message) bool { return m.Deleted })
	if msg.Text != "" || msg.Edited {
		t.Errorf("deleted message = %+v, want no text", msg)
	}
	// Deleting again changes nothing.
	call[api.Empty](t, c, api.CommandMessagesDelete, api.MessagesDeleteParams{ConversationID: conv.ConversationID, MessageID: sent.MessageID})

	// What the commands refuse.
	callFails(t, c, api.CommandMessagesEdit, api.MessagesEditParams{ConversationID: conv.ConversationID, MessageID: echoed.MessageID, Text: "not mine"}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandMessagesDelete, api.MessagesDeleteParams{ConversationID: conv.ConversationID, MessageID: echoed.MessageID}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandMessagesEdit, api.MessagesEditParams{ConversationID: conv.ConversationID, MessageID: sent.MessageID, Text: "deleted"}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandMessagesEdit, api.MessagesEditParams{ConversationID: conv.ConversationID, MessageID: answer.MessageID}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandMessagesReact, api.MessagesReactParams{ConversationID: conv.ConversationID, MessageID: sent.MessageID, Key: "👍"}, api.ErrorCodeInvalidParams)
	other := conversationNamed(t, c, "Unreachable Contact")
	callFails(t, c, api.CommandMessagesReact, api.MessagesReactParams{ConversationID: other.ConversationID, MessageID: echoed.MessageID, Key: "👍"}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: other.ConversationID, Text: "hi", ReplyTo: echoed.MessageID}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandMessagesReact, api.MessagesReactParams{ConversationID: conv.ConversationID, MessageID: echoed.MessageID}, api.ErrorCodeInvalidParams)

	// The history has each message once, as changed (the answer's echo may
	// follow).
	list := call[api.MessagesListResult](t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID})
	var texts []string
	for _, m := range list.Messages[:min(3, len(list.Messages))] {
		texts = append(texts, m.Text)
	}
	if want := []string{"", echoed.Text, "an answer"}; !slices.Equal(texts, want) {
		t.Errorf("history = %q, want %q: %+v", texts, want, list.Messages)
	}
}

// TestRefusedChangesAreUndone makes changes that the network refuses: the
// command fails with network, and the reaction or the edit is gone again.
func TestRefusedChangesAreUndone(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Unreachable Contact")
	sent := call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: "anyone?"}).Message
	waitEvent(t, c, "the failure", messageEvent(t, api.EventMessageUpdated, func(m api.Message) bool {
		return m.MessageID == sent.MessageID && m.Status == api.MessageStatusFailed
	}))

	callFails(t, c, api.CommandMessagesReact, api.MessagesReactParams{ConversationID: conv.ConversationID, MessageID: sent.MessageID, Key: "👍"}, api.ErrorCodeNetwork)
	callFails(t, c, api.CommandMessagesEdit, api.MessagesEditParams{ConversationID: conv.ConversationID, MessageID: sent.MessageID, Text: "anybody?"}, api.ErrorCodeNetwork)
	list := call[api.MessagesListResult](t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID})
	if len(list.Messages) != 1 {
		t.Fatalf("history = %+v, want the message alone", list.Messages)
	}
	if m := list.Messages[0]; m.Text != "anyone?" || m.Edited || len(m.Reactions) != 0 {
		t.Errorf("message = %+v, want it unchanged", m)
	}
	// A deletion stays on this device even when refused.
	callFails(t, c, api.CommandMessagesDelete, api.MessagesDeleteParams{ConversationID: conv.ConversationID, MessageID: sent.MessageID}, api.ErrorCodeNetwork)
	list = call[api.MessagesListResult](t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID})
	if len(list.Messages) != 1 || !list.Messages[0].Deleted {
		t.Errorf("history = %+v, want the message deleted", list.Messages)
	}
}
