// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"testing"

	"github.com/quentinemusee/musubee/core/api"
)

// TestMembers lists the echo group's members: the user first, then the
// two contacts by name, without the bridge's bot (API 1.27).
func TestMembers(t *testing.T) {
	c := open(t)
	login(t, c)
	group := conversationNamed(t, c, "Echo Group")
	members := call[api.ConversationsMembersResult](t, c, api.CommandConversationsMembers, api.ConversationsMembersParams{ConversationID: group.ConversationID}).Members
	if len(members) != 3 || !members[0].Me || members[1] != (api.ConversationMember{Name: "Delayed Echo"}) || members[2] != (api.ConversationMember{Name: "Instant Echo"}) {
		t.Errorf("members = %+v, want the user, Delayed Echo and Instant Echo", members)
	}
	direct := conversationNamed(t, c, "Instant Echo")
	if members := call[api.ConversationsMembersResult](t, c, api.CommandConversationsMembers, api.ConversationsMembersParams{ConversationID: direct.ConversationID}).Members; len(members) != 2 || !members[0].Me || members[1].Name != "Instant Echo" {
		t.Errorf("direct conversation's members = %+v", members)
	}
	callFails(t, c, api.CommandConversationsMembers, api.ConversationsMembersParams{ConversationID: "c.nope"}, api.ErrorCodeNotFound)
}
