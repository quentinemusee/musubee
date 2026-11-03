// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"testing"

	"github.com/quentinemusee/musubee/core/api"
)

// TestPin: a pinned conversation leads the first page of the list, and no
// later page repeats it (API 1.24, docs/ADR/0052).
func TestPin(t *testing.T) {
	c := open(t)
	login(t, c)
	// The least recently active conversation, so that pinning moves it.
	all := waitConversations(t, c, conversationsPerLogin)
	last := all[len(all)-1]
	if last.Pinned {
		t.Fatalf("%s is pinned before any pin", last.Name)
	}
	pin := func(pinned bool) {
		t.Helper()
		call[api.Empty](t, c, api.CommandConversationsPin, api.ConversationsPinParams{ConversationID: last.ConversationID, Pinned: pinned})
	}
	pin(true)
	waitConversationUpdate(t, c, "pinned", last.ConversationID, func(cv api.Conversation) bool { return cv.Pinned })

	// Page by page, one at a time.
	var seen []string
	before := ""
	for range len(all) + 1 {
		page := call[api.ConversationsListResult](t, c, api.CommandConversationsList, api.ConversationsListParams{Limit: 1, Before: before})
		for _, conv := range page.Conversations {
			seen = append(seen, conv.ConversationID)
		}
		if before = page.Before; before == "" {
			break
		}
	}
	if len(seen) != len(all) || seen[0] != last.ConversationID {
		t.Fatalf("pages = %v, want %d conversations starting with %s", seen, len(all), last.ConversationID)
	}
	for _, id := range seen[1:] {
		if id == last.ConversationID {
			t.Errorf("the pinned conversation came again on a later page: %v", seen)
		}
	}

	pin(false)
	waitConversationUpdate(t, c, "unpinned", last.ConversationID, func(cv api.Conversation) bool { return !cv.Pinned })
	if first := call[api.ConversationsListResult](t, c, api.CommandConversationsList, nil).Conversations[0]; first.ConversationID == last.ConversationID {
		t.Errorf("an unpinned conversation still leads the list")
	}
	callFails(t, c, api.CommandConversationsPin, api.ConversationsPinParams{ConversationID: "c.nope", Pinned: true}, api.ErrorCodeNotFound)
}
