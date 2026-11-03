// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package localmatrix_test

import (
	"testing"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/localmatrix"
)

// TestUnreadMentions counts the unread messages that mention the user, by
// ID or the whole room, and forgets them once read (docs/ADR/0062).
func TestUnreadMentions(t *testing.T) {
	server := newServer(t, localmatrix.Options{})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	roomID := createDM(t, server, conn, "alice")
	alice := conn.GhostIntent("alice")
	send := func(body string, mentions *event.Mentions) id.EventID {
		t.Helper()
		resp, err := alice.SendMessage(ctx, roomID, event.EventMessage, &event.Content{
			Parsed: &event.MessageEventContent{MsgType: event.MsgText, Body: body, Mentions: mentions},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return resp.EventID
	}
	expect := func(what string, unread, mentions int) {
		t.Helper()
		st, err := server.ReadState(ctx, roomID)
		if err != nil {
			t.Fatal(err)
		}
		if st.Unread != unread || st.UnreadMentions != mentions {
			t.Errorf("%s: %d unread, %d mentions; want %d and %d", what, st.Unread, st.UnreadMentions, unread, mentions)
		}
	}
	send("plain", nil)
	send("for someone else", &event.Mentions{UserIDs: []id.UserID{"@other:musubee.local"}})
	expect("no mention of the user", 2, 0)
	send("hey you", &event.Mentions{UserIDs: []id.UserID{server.UserID()}})
	last := send("everyone", &event.Mentions{Room: true})
	expect("two mentions", 4, 2)
	if err := server.MarkRead(ctx, roomID, last); err != nil {
		t.Fatal(err)
	}
	expect("read", 0, 0)
}
