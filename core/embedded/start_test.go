// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"testing"

	"github.com/quentinemusee/musubee/core/api"
)

// TestStartConversation deletes a conversation, then starts it again from
// the contact's name (API 1.37, docs/ADR/0069).
func TestStartConversation(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Delayed Echo")
	accounts := call[api.AccountsListResult](t, c, api.CommandAccountsList, nil).Accounts
	if len(accounts) != 1 || !accounts[0].StartsConversations {
		t.Fatalf("the echo account does not start conversations: %+v", accounts)
	}
	call[api.Empty](t, c, api.CommandConversationsDelete, api.ConversationsDeleteParams{ConversationID: conv.ConversationID})

	started := call[api.ConversationsStartResult](t, c, api.CommandConversationsStart, api.ConversationsStartParams{AccountID: conv.AccountID, Identifier: "  delayed echo "}).Conversation
	if started.Name != "Delayed Echo" || started.Kind != api.ConversationKindDirect || started.AccountID != conv.AccountID {
		t.Errorf("started conversation = %+v", started)
	}
	// Starting it again opens the same one.
	again := call[api.ConversationsStartResult](t, c, api.CommandConversationsStart, api.ConversationsStartParams{AccountID: conv.AccountID, Identifier: "Delayed Echo"}).Conversation
	if again.ConversationID != started.ConversationID {
		t.Errorf("starting again opened %s, want %s", again.ConversationID, started.ConversationID)
	}
	callFails(t, c, api.CommandConversationsStart, api.ConversationsStartParams{AccountID: conv.AccountID, Identifier: "nobody at all"}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandConversationsStart, api.ConversationsStartParams{AccountID: "a.nope", Identifier: "x"}, api.ErrorCodeNotFound)
}
