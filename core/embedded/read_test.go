// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"testing"
	"time"

	"github.com/quentinemusee/musubee/core/api"
)

// waitConversationUpdate waits for a conversation.updated event of a
// conversation that matches.
func waitConversationUpdate(t *testing.T, c *Core, what, conversation string, match func(api.Conversation) bool) api.Conversation {
	t.Helper()
	e := waitEvent(t, c, what, func(e rawEvent) bool {
		if e.Type != api.EventConversationUpdated {
			return false
		}
		conv := decode[api.ConversationEvent](t, e.Data).Conversation
		return conv.ConversationID == conversation && match(conv)
	})
	return decode[api.ConversationEvent](t, e.Data).Conversation
}

func TestReadState(t *testing.T) {
	c := open(t)
	accountID := login(t, c)
	conv := conversationNamed(t, c, "Instant Echo")
	if conv.UnreadCount != 0 || conv.MarkedUnread {
		t.Fatalf("new conversation = %+v, want nothing unread", conv)
	}

	call[api.MessagesSendResult](t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv.ConversationID, Text: "one"})
	unread := waitConversationUpdate(t, c, "the echo, unread", conv.ConversationID, func(cv api.Conversation) bool {
		return cv.UnreadCount == 1
	})
	echo := unread.LastMessage
	if echo == nil || echo.FromMe {
		t.Fatalf("conversation with an unread echo = %+v, want the echo as its last message", unread)
	}
	listed := conversationNamed(t, c, "Instant Echo")
	if listed.UnreadCount != 1 {
		t.Errorf("conversations.list: %d unread, want 1", listed.UnreadCount)
	}

	call[api.Empty](t, c, api.CommandConversationsMarkRead, api.ConversationsMarkReadParams{
		ConversationID: conv.ConversationID, MessageID: echo.MessageID,
	})
	waitConversationUpdate(t, c, "the conversation, read", conv.ConversationID, func(cv api.Conversation) bool {
		return cv.UnreadCount == 0
	})
	if listed = conversationNamed(t, c, "Instant Echo"); listed.UnreadCount != 0 {
		t.Errorf("conversations.list after reading: %d unread, want 0", listed.UnreadCount)
	}

	// The receipt reached the network: bridgev2 records how far the
	// account read once the connector accepted it.
	roomID, err := parseConversationID(conv.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	network, loginID, err := parseAccountID(accountID)
	if err != nil {
		t.Fatal(err)
	}
	br := c.host.Bridge(network)
	portal, err := br.GetPortalByMXID(t.Context(), roomID)
	if err != nil || portal == nil {
		t.Fatalf("portal = %v, %v", portal, err)
	}
	userLogin := br.GetCachedUserLoginByID(loginID)
	deadline := time.Now().Add(eventTimeout)
	for {
		up, err := br.DB.UserPortal.Get(t.Context(), userLogin.UserLogin, portal.PortalKey)
		if err != nil {
			t.Fatal(err)
		}
		if up != nil && up.LastRead.UnixMilli() == echo.TimestampMS {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the network's read position = %+v, want the echo's time %d", up, echo.TimestampMS)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The default is the last message; reading again is harmless.
	call[api.Empty](t, c, api.CommandConversationsMarkRead, api.ConversationsMarkReadParams{ConversationID: conv.ConversationID})

	other := conversationNamed(t, c, "Delayed Echo")
	callFails(t, c, api.CommandConversationsMarkRead, api.ConversationsMarkReadParams{
		ConversationID: other.ConversationID, MessageID: echo.MessageID,
	}, api.ErrorCodeNotFound)
	callFails(t, c, api.CommandConversationsMarkRead, api.ConversationsMarkReadParams{
		ConversationID: conv.ConversationID, MessageID: "m.bm9wZQ",
	}, api.ErrorCodeNotFound)
	// A conversation without messages has nothing to read.
	call[api.Empty](t, c, api.CommandConversationsMarkRead, api.ConversationsMarkReadParams{ConversationID: other.ConversationID})
}
