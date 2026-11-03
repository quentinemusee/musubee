// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"testing"

	"github.com/quentinemusee/musubee/core/api"
)

// TestDeleteChat: the echo network deletes a conversation for the user
// (API 1.25, docs/ADR/0053); it goes from the list with its messages.
func TestDeleteChat(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Delayed Echo")
	if conv.Actions == nil || !conv.Actions.DeleteChat {
		t.Fatalf("the echo network does not offer to delete a conversation: %+v", conv.Actions)
	}
	call[api.Empty](t, c, api.CommandConversationsDelete, api.ConversationsDeleteParams{ConversationID: conv.ConversationID})
	waitEvent(t, c, "conversation.deleted", func(e rawEvent) bool {
		return e.Type == api.EventConversationDeleted && decode[api.ConversationDeletedEvent](t, e.Data).ConversationID == conv.ConversationID
	})
	for _, other := range call[api.ConversationsListResult](t, c, api.CommandConversationsList, nil).Conversations {
		if other.ConversationID == conv.ConversationID {
			t.Errorf("the deleted conversation is still listed")
		}
	}
	callFails(t, c, api.CommandMessagesList, api.MessagesListParams{ConversationID: conv.ConversationID}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandConversationsDelete, api.ConversationsDeleteParams{ConversationID: conv.ConversationID}, api.ErrorCodeNotFound)
}
