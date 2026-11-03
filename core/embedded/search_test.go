// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"slices"
	"testing"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/connector/echo"
)

// TestSearchMessages finds messages by their text (API 1.17): in one
// conversation or all, newest first, by their latest text, ignoring ASCII
// case, in pages; never deleted ones.
func TestSearchMessages(t *testing.T) {
	c := open(t)
	login(t, c)
	instant := conversationNamed(t, c, "Instant Echo")
	group := conversationNamed(t, c, "Echo Group")
	send := func(conv api.Conversation, text string) {
		t.Helper()
		call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: text})
		waitEvent(t, c, "the echo of "+text, messageEvent(t, api.EventMessageAdded, func(m api.Message) bool { return !m.FromMe && m.ConversationID == conv.ConversationID }))
	}
	send(instant, "Picnic on Sunday")
	send(group, "the picnic basket")
	send(instant, echo.CommandEdit+" a garden party")
	waitEvent(t, c, "the edit", messageEvent(t, api.EventMessageUpdated, func(m api.Message) bool { return m.Edited }))
	send(instant, echo.CommandDelete+" a secret picnic")
	waitEvent(t, c, "the deletion", messageEvent(t, api.EventMessageUpdated, func(m api.Message) bool { return m.Deleted }))

	search := func(query, conversation string, limit int64, before string) api.MessagesSearchResult {
		t.Helper()
		return call[api.MessagesSearchResult](t, c, api.CommandMessagesSearch, api.MessagesSearchParams{Query: query, ConversationID: conversation, Limit: limit, Before: before})
	}
	texts := func(r api.MessagesSearchResult) []string {
		var out []string
		for _, m := range r.Messages {
			out = append(out, m.Text)
		}
		return out
	}
	// Everywhere, newest first, any case; the deleted echo never (the
	// user's own command stays).
	if got, want := texts(search("PICNIC", "", 0, "")), []string{"/delete a secret picnic", "the picnic basket", "the picnic basket", "Picnic on Sunday", "Picnic on Sunday"}; !slices.Equal(got, want) {
		t.Errorf("everywhere = %q, want %q", got, want)
	}
	if got, want := texts(search("picnic", instant.ConversationID, 0, "")), []string{"/delete a secret picnic", "Picnic on Sunday", "Picnic on Sunday"}; !slices.Equal(got, want) {
		t.Errorf("in one conversation = %q, want %q", got, want)
	}
	// By the latest text: the echo's edit says "(edited)".
	if got := texts(search("edited", "", 0, "")); !slices.Equal(got, []string{"a garden party" + echo.EditedSuffix}) {
		t.Errorf("by an edit's text = %q", got)
	}
	// Pages.
	first := search("picnic", "", 3, "")
	if len(first.Messages) != 3 || first.Before == "" {
		t.Fatalf("first page = %q, before %q", texts(first), first.Before)
	}
	if rest := search("picnic", "", 3, first.Before); len(rest.Messages) != 2 || rest.Before != "" {
		t.Errorf("second page = %q, before %q", texts(rest), rest.Before)
	}
	// Characters of LIKE are plain text.
	if got := search("100%", "", 0, ""); len(got.Messages) != 0 {
		t.Errorf("%% matched %q", texts(got))
	}
	callFails(t, c, api.CommandMessagesSearch, api.MessagesSearchParams{Query: "   "}, api.ErrorCodeInvalidParams)
	callFails(t, c, api.CommandMessagesSearch, api.MessagesSearchParams{Query: "x", ConversationID: "c.nope"}, api.ErrorCodeNotFound)
}
