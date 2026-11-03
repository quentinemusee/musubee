// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"testing"

	"github.com/quentinemusee/musubee/core/api"
)

// TestDraft keeps a conversation's unsent text in the core, where the list
// gives it back, and forgets it (API 1.30, docs/ADR/0057).
func TestDraft(t *testing.T) {
	c := open(t)
	login(t, c)
	conv := conversationNamed(t, c, "Delayed Echo")
	draftOf := func() string {
		t.Helper()
		for _, other := range call[api.ConversationsListResult](t, c, api.CommandConversationsList, nil).Conversations {
			if other.ConversationID == conv.ConversationID {
				return other.Draft
			}
		}
		t.Fatal("the conversation is not listed")
		return ""
	}
	setDraft := func(text string) {
		t.Helper()
		call[api.Empty](t, c, api.CommandConversationsSetDraft, api.ConversationsSetDraftParams{ConversationID: conv.ConversationID, Text: text})
	}
	setDraft("half a thought")
	if got := draftOf(); got != "half a thought" {
		t.Errorf("draft = %q", got)
	}
	setDraft("")
	if got := draftOf(); got != "" {
		t.Errorf("forgotten draft = %q", got)
	}
	callFails(t, c, api.CommandConversationsSetDraft, api.ConversationsSetDraftParams{ConversationID: "c.nope", Text: "x"}, api.ErrorCodeNotFound)
}
