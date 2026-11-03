// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"testing"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/connector/echo"
)

// TestMentions: the echo contact's answer to /mention mentions the user,
// a plain echo does not (API 1.28, docs/ADR/0055).
func TestMentions(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Instant Echo")
	answer := func(text, want string) api.Message {
		t.Helper()
		call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: text})
		e := waitEvent(t, c, want, messageEvent(t, api.EventMessageAdded, func(m api.Message) bool { return !m.FromMe && m.Text == want }))
		return decode[api.MessageEvent](t, e.Data).Message
	}
	if plain := answer("hello", "hello"); plain.MentionsMe {
		t.Errorf("a plain echo mentions the user: %+v", plain)
	}
	if mention := answer(echo.CommandMention+" look at this", "look at this"); !mention.MentionsMe {
		t.Errorf("the answer to /mention does not mention the user: %+v", mention)
	}
}

func TestMentionsOf(t *testing.T) {
	me := id.UserID("@me:musubee.local")
	for _, tc := range []struct {
		content *event.MessageEventContent
		want    bool
	}{
		{nil, false},
		{&event.MessageEventContent{}, false},
		{&event.MessageEventContent{Mentions: &event.Mentions{UserIDs: []id.UserID{"@other:x"}}}, false},
		{&event.MessageEventContent{Mentions: &event.Mentions{UserIDs: []id.UserID{"@other:x", me}}}, true},
		{&event.MessageEventContent{Mentions: &event.Mentions{Room: true}}, true},
	} {
		if got := mentions(tc.content, me); got != tc.want {
			t.Errorf("mentions(%+v) = %v, want %v", tc.content, got, tc.want)
		}
	}
}
