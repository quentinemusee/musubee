// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package localmatrix_test

import (
	"slices"
	"testing"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/localmatrix"
)

// batchEvent is a message of a batch as bridgev2 makes it: a deterministic
// ID, a sender and a timestamp.
func batchEvent(eventID id.EventID, sender id.UserID, body string, ts int64) *event.Event {
	return &event.Event{
		ID:        eventID,
		Sender:    sender,
		Type:      event.EventMessage,
		Timestamp: ts,
		Content:   event.Content{Parsed: text(body)},
	}
}

func TestBatchSendHistory(t *testing.T) {
	server := newServer(t, localmatrix.Options{UserLocalpart: "me"})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	roomID := createDM(t, server, conn, "alice")
	other := createDM(t, server, conn, "bob")
	alice := conn.FormatGhostMXID("alice")
	send(t, conn.GhostIntent("alice"), roomID, event.EventMessage, text("now"))

	batch := func(req *mautrix.ReqBeeperBatchSend, room id.RoomID) {
		t.Helper()
		resp, err := conn.BatchSend(ctx, room, req, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.EventIDs) != len(req.Events) {
			t.Fatalf("%d event IDs for %d events", len(resp.EventIDs), len(req.Events))
		}
	}
	// Older history, twice: each batch goes before what is stored, even
	// after another room's history.
	batch(&mautrix.ReqBeeperBatchSend{Events: []*event.Event{
		batchEvent("$h3", alice, "older 3", 3000), batchEvent("$h4", server.UserID(), "older 4", 4000),
	}}, roomID)
	batch(&mautrix.ReqBeeperBatchSend{Events: []*event.Event{batchEvent("$b1", conn.FormatGhostMXID("bob"), "bob's", 1000)}}, other)
	batch(&mautrix.ReqBeeperBatchSend{Events: []*event.Event{
		batchEvent("$h1", alice, "older 1", 1000), batchEvent("$h2", alice, "older 2", 2000),
		// Already stored: skipped.
		batchEvent("$h3", alice, "older 3", 3000),
	}}, roomID)
	// A forward batch goes after.
	batch(&mautrix.ReqBeeperBatchSend{Forward: true, Events: []*event.Event{batchEvent("$f1", alice, "later", 9_000_000_000_000)}}, roomID)

	messages, err := server.MessagesBefore(ctx, roomID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(messages)
	if got, want := bodies(messages), []string{"older 1", "older 2", "older 3", "older 4", "now", "later"}; !slices.Equal(got, want) {
		t.Fatalf("history = %q, want %q", got, want)
	}
	// Pages of older messages from a cursor below 0.
	page, err := server.MessagesBefore(ctx, roomID, messages[2].Unsigned.BeeperHSOrder, 10)
	if err != nil || !slices.Equal(bodies(page), []string{"older 2", "older 1"}) {
		t.Errorf("before older 3 = %q (%v)", bodies(page), err)
	}
	if oldest, err := server.OldestMessageOrder(ctx, roomID); err != nil || oldest != messages[0].Unsigned.BeeperHSOrder || oldest >= 0 {
		t.Errorf("oldest = %d (%v), want older 1's, below 0", oldest, err)
	}
	// History is not news; the forward message is.
	if read, err := server.ReadState(ctx, roomID); err != nil || read.Unread != 2 {
		t.Errorf("unread = %+v (%v), want 2: \"now\" and \"later\"", read, err)
	}
	if read, err := server.ReadState(ctx, other); err != nil || read.Unread != 0 {
		t.Errorf("unread in bob's room = %+v (%v), want 0", read, err)
	}
}

func TestBatchSendForwardIfNoMessages(t *testing.T) {
	server := newServer(t, localmatrix.Options{UserLocalpart: "me"})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	roomID := createDM(t, server, conn, "alice")
	alice := conn.FormatGhostMXID("alice")
	updates, stop := server.Subscribe(16)
	defer stop()

	resp, err := conn.BatchSend(ctx, roomID, &mautrix.ReqBeeperBatchSend{ForwardIfNoMessages: true, Events: []*event.Event{
		batchEvent("$a", alice, "first", 1000), batchEvent("$b", alice, "second", 2000),
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Forward in an empty room: published like any message, but read.
	published := 0
	for len(updates) > 0 {
		if u := <-updates; u.Event != nil && u.Event.Type == event.EventMessage {
			published++
		}
	}
	if published != 2 {
		t.Errorf("%d messages published, want 2", published)
	}
	messages, err := server.MessagesBefore(ctx, roomID, 0, 10)
	if err != nil || len(messages) != 2 || messages[0].ID != resp.EventIDs[1] || messages[0].Unsigned.BeeperHSOrder <= 0 {
		t.Errorf("messages = %v (%v), want both, forward", messages, err)
	}
	if read, err := server.ReadState(ctx, roomID); err != nil || read.Unread != 0 {
		t.Errorf("unread = %+v (%v), want 0: history the user asked for", read, err)
	}
}
