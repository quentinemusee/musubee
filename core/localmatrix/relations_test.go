// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package localmatrix_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/localmatrix"
	"github.com/quentinemusee/musubee/core/storage/sqlite"
)

func send(t *testing.T, intent bridgev2.MatrixAPI, roomID id.RoomID, eventType event.Type, parsed any) id.EventID {
	t.Helper()
	resp, err := intent.SendMessage(t.Context(), roomID, eventType, &event.Content{Parsed: parsed}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return resp.EventID
}

func text(body string) *event.MessageEventContent {
	return &event.MessageEventContent{MsgType: event.MsgText, Body: body}
}

// edit is an edit as bridgev2 sends it: the new content in m.new_content,
// and a "* " fallback in the body.
func edit(target id.EventID, body string) *event.MessageEventContent {
	content := text(body)
	content.SetEdit(target)
	return content
}

func reaction(target id.EventID, key string) *event.ReactionEventContent {
	return &event.ReactionEventContent{RelatesTo: event.RelatesTo{Type: event.RelAnnotation, EventID: target, Key: key}}
}

func redaction(target id.EventID) *event.RedactionEventContent {
	return &event.RedactionEventContent{Redacts: target}
}

func bodies(events []*event.Event) []string {
	var out []string
	for _, evt := range events {
		out = append(out, evt.Content.AsMessage().Body)
	}
	return out
}

func TestRelations(t *testing.T) {
	server := newServer(t, localmatrix.Options{UserLocalpart: "me"})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	roomID := createDM(t, server, conn, "alice")
	alice, bob := conn.GhostIntent("alice"), conn.GhostIntent("bob")

	first := send(t, alice, roomID, event.EventMessage, text("first"))
	second := send(t, alice, roomID, event.EventMessage, text("second"))
	send(t, alice, roomID, event.EventMessage, edit(first, "first, edited"))
	send(t, alice, roomID, event.EventMessage, edit(first, "first, edited again"))
	// Only the sender can edit a message.
	send(t, bob, roomID, event.EventMessage, edit(first, "first, edited by bob"))
	send(t, alice, roomID, event.EventReaction, reaction(first, "👍"))
	send(t, bob, roomID, event.EventReaction, reaction(first, "❤️"))
	send(t, bob, roomID, event.EventReaction, reaction(first, "👍"))
	// The same key twice from one sender counts once.
	send(t, alice, roomID, event.EventReaction, reaction(first, "👍"))
	taken := send(t, alice, roomID, event.EventReaction, reaction(second, "😮"))
	send(t, alice, roomID, event.EventRedaction, redaction(taken))

	// Edits are not messages of their own.
	messages, err := server.MessagesBefore(ctx, roomID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(messages); !slices.Equal(got, []string{"second", "first"}) {
		t.Fatalf("messages = %q, want second and first", got)
	}
	activity, err := server.RoomActivity(ctx, roomID)
	if err != nil {
		t.Fatal(err)
	}
	if activity.LastMessage == nil || activity.LastMessage.ID != second {
		t.Errorf("last message = %v, want the second one", activity.LastMessage)
	}
	read, err := server.ActivityReadState(ctx, activity)
	if err != nil || read.Unread != 2 {
		t.Errorf("unread = %+v (%v), want 2: edits are not new messages", read, err)
	}

	relations, err := server.Relations(ctx, roomID, messages)
	if err != nil {
		t.Fatal(err)
	}
	rel := relations[first]
	if rel == nil || rel.Edit == nil {
		t.Fatalf("relations of the first message = %+v, want an edit", rel)
	}
	if body := rel.Edit.Content.AsMessage().NewContent.Body; body != "first, edited again" {
		t.Errorf("edit = %q, want the latest one by the sender", body)
	}
	want := []localmatrix.Reaction{
		{Key: "👍", Senders: []id.UserID{alice.GetMXID(), bob.GetMXID()}},
		{Key: "❤️", Senders: []id.UserID{bob.GetMXID()}},
	}
	if !reactionsEqual(rel.Reactions, want) {
		t.Errorf("reactions = %+v, want %+v", rel.Reactions, want)
	}
	if rel, ok := relations[second]; ok {
		t.Errorf("relations of the second message = %+v, want none: its reaction was redacted", rel)
	}
}

func reactionsEqual(a, b []localmatrix.Reaction) bool {
	return slices.EqualFunc(a, b, func(x, y localmatrix.Reaction) bool {
		return x.Key == y.Key && slices.Equal(x.Senders, y.Senders)
	})
}

// TestRedactionErasesEdits checks that deleting a message deletes the text
// of its edits too, and that a redacted event keeps its relation.
func TestRedactionErasesEdits(t *testing.T) {
	server := newServer(t, localmatrix.Options{UserLocalpart: "me"})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	roomID := createDM(t, server, conn, "alice")
	alice := conn.GhostIntent("alice")

	msg := send(t, alice, roomID, event.EventMessage, text("secret"))
	edited := send(t, alice, roomID, event.EventMessage, edit(msg, "secret, edited"))
	removal := send(t, alice, roomID, event.EventRedaction, redaction(msg))

	for _, eventID := range []id.EventID{msg, edited} {
		evt, err := server.Event(ctx, roomID, eventID)
		if err != nil {
			t.Fatal(err)
		}
		content := evt.Content.AsMessage()
		if content.Body != "" || content.NewContent != nil {
			t.Errorf("%s still has text after the redaction: %+v", eventID, content)
		}
		if evt.Unsigned.RedactedBecause == nil || evt.Unsigned.RedactedBecause.ID != removal {
			t.Errorf("%s: RedactedBecause = %v, want %s", eventID, evt.Unsigned.RedactedBecause, removal)
		}
	}
	evt, err := server.Event(ctx, roomID, edited)
	if err != nil {
		t.Fatal(err)
	}
	if target := evt.Content.AsMessage().RelatesTo.GetReplaceID(); target != msg {
		t.Errorf("the redacted edit relates to %q, want %s", target, msg)
	}
	// The deleted message stays in the history, without its edit.
	messages, err := server.MessagesBefore(ctx, roomID, 0, 10)
	if err != nil || len(messages) != 1 || messages[0].ID != msg {
		t.Fatalf("messages = %v (%v), want the deleted one alone", messages, err)
	}
	relations, err := server.Relations(ctx, roomID, messages)
	if err != nil || len(relations) != 0 {
		t.Errorf("relations = %v (%v), want none", relations, err)
	}
	read, err := server.ReadState(ctx, roomID)
	if err != nil || read.Unread != 0 {
		t.Errorf("unread = %+v (%v), want 0: a deleted message is not news", read, err)
	}
}

// dropRelations takes a database back to before version 4.
const dropRelations = `DROP INDEX local_event_relation_idx;
	ALTER TABLE local_event DROP COLUMN rel_type;
	ALTER TABLE local_event DROP COLUMN relates_to;
	ALTER TABLE local_event DROP COLUMN redacted_by;
	`

// TestUpgradeFromV3 checks the migration that adds the relation columns to
// a database written before them, where a redaction left the edits.
func TestUpgradeFromV3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.db")
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	server := localmatrix.New(db, localmatrix.Options{Log: zerolog.Nop(), UserLocalpart: "me"})
	if err = server.Upgrade(t.Context()); err != nil {
		t.Fatal(err)
	}
	conn := newConnector(t, server, "echo")
	roomID := createDM(t, server, conn, "alice")
	alice := conn.GhostIntent("alice")
	kept := send(t, alice, roomID, event.EventMessage, text("kept"))
	send(t, alice, roomID, event.EventMessage, edit(kept, "kept, edited"))
	send(t, alice, roomID, event.EventReaction, reaction(kept, "👍"))
	deleted := send(t, alice, roomID, event.EventMessage, text("deleted"))
	deletedEdit := send(t, alice, roomID, event.EventMessage, edit(deleted, "deleted, edited"))
	removal := send(t, alice, roomID, event.EventRedaction, redaction(deleted))
	// Back to version 3, as the core of T2.17 left it: the redaction erased
	// the message only.
	_, err = db.Exec(t.Context(), `
		UPDATE local_event SET content=$1 WHERE event_id=$2;
		`+dropRelations+`UPDATE local_version SET version=3`,
		`{"msgtype":"m.text","body":"* deleted, edited","m.new_content":{"msgtype":"m.text","body":"deleted, edited"},"m.relates_to":{"rel_type":"m.replace","event_id":"`+string(deleted)+`"}}`,
		deletedEdit)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	db, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	server = localmatrix.New(db, localmatrix.Options{Log: zerolog.Nop(), UserLocalpart: "me"})
	if err = server.Upgrade(t.Context()); err != nil {
		t.Fatal(err)
	}
	messages, err := server.MessagesBefore(t.Context(), roomID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].ID != deleted || messages[1].ID != kept {
		t.Fatalf("messages = %q, want deleted and kept, without the edits", bodies(messages))
	}
	if because := messages[0].Unsigned.RedactedBecause; because == nil || because.ID != removal {
		t.Errorf("the deleted message: RedactedBecause = %v, want %s", because, removal)
	}
	relations, err := server.Relations(t.Context(), roomID, messages)
	if err != nil {
		t.Fatal(err)
	}
	if rel := relations[kept]; rel == nil || rel.Edit == nil || len(rel.Reactions) != 1 {
		t.Errorf("relations of the kept message = %+v, want its edit and reaction", rel)
	}
	evt, err := server.Event(t.Context(), roomID, deletedEdit)
	if err != nil {
		t.Fatal(err)
	}
	if content := evt.Content.AsMessage(); content.Body != "" || content.NewContent != nil || content.RelatesTo.GetReplaceID() != deleted {
		t.Errorf("the edit of the deleted message after the upgrade: %+v, want only its relation", content)
	}
}
