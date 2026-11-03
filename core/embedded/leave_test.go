// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"testing"

	"github.com/quentinemusee/musubee/core/api"
)

// TestLeave leaves the echo group: the network accepts, and the group goes
// from the device (API 1.29, docs/ADR/0056). A direct conversation cannot
// be left.
func TestLeave(t *testing.T) {
	c := open(t)
	login(t, c)
	group := conversationNamed(t, c, "Echo Group")
	if group.Actions == nil || !group.Actions.Leave {
		t.Fatalf("the echo group cannot be left: %+v", group.Actions)
	}
	direct := conversationNamed(t, c, "Instant Echo")
	if direct.Actions != nil && direct.Actions.Leave {
		t.Errorf("a direct conversation offers to leave")
	}
	callFails(t, c, api.CommandConversationsLeave, api.ConversationsLeaveParams{ConversationID: direct.ConversationID}, api.ErrorCodeUnsupported)

	call[api.Empty](t, c, api.CommandConversationsLeave, api.ConversationsLeaveParams{ConversationID: group.ConversationID})
	waitEvent(t, c, "conversation.deleted", func(e rawEvent) bool {
		return e.Type == api.EventConversationDeleted && decode[api.ConversationDeletedEvent](t, e.Data).ConversationID == group.ConversationID
	})
	for _, conv := range call[api.ConversationsListResult](t, c, api.CommandConversationsList, nil).Conversations {
		if conv.ConversationID == group.ConversationID {
			t.Errorf("the group left is still listed")
		}
	}
	callFails(t, c, api.CommandConversationsLeave, api.ConversationsLeaveParams{ConversationID: group.ConversationID}, api.ErrorCodeNotFound)
}
