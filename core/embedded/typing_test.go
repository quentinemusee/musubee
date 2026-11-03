// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"slices"
	"testing"

	"github.com/quentinemusee/musubee/core/api"
)

// TestTyping tells the echo network that the user types (API 1.20): its
// contact types back, which the core announces, then stops by itself.
func TestTyping(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Instant Echo")
	typing := func(what string, want []string) {
		t.Helper()
		waitEvent(t, c, what, func(e rawEvent) bool {
			if e.Type != api.EventConversationTyping {
				return false
			}
			data := decode[api.TypingEvent](t, e.Data)
			return data.ConversationID == conv.ConversationID && slices.Equal(data.Names, want)
		})
	}
	call[api.Empty](t, c, api.CommandConversationsTyping, api.ConversationsTypingParams{ConversationID: conv.ConversationID, Typing: true})
	typing("the contact typing", []string{"Instant Echo"})
	// The echo network's contact types for three seconds.
	typing("the contact done", []string{})
	call[api.Empty](t, c, api.CommandConversationsTyping, api.ConversationsTypingParams{ConversationID: conv.ConversationID, Typing: false})
	callFails(t, c, api.CommandConversationsTyping, api.ConversationsTypingParams{ConversationID: "c.nope", Typing: true}, api.ErrorCodeNotFound)
}
