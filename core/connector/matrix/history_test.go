// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package matrix

import (
	"testing"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestHistoryChanges(t *testing.T) {
	msg := func(eventID id.EventID, sender id.UserID, ts int64, content *event.MessageEventContent) *event.Event {
		return &event.Event{ID: eventID, Sender: sender, Timestamp: ts, Type: event.EventMessage, Content: event.Content{Parsed: content}}
	}
	edit := func(eventID, target id.EventID, sender id.UserID, ts int64, text string) *event.Event {
		content := &event.MessageEventContent{MsgType: event.MsgText, Body: "* " + text}
		content.SetEdit(target)
		content.NewContent = &event.MessageEventContent{MsgType: event.MsgText, Body: text}
		return msg(eventID, sender, ts, content)
	}
	reaction := &event.Event{ID: "$r", Sender: "@carol:x", Type: event.EventReaction, Content: event.Content{Parsed: &event.ReactionEventContent{
		RelatesTo: event.RelatesTo{Type: event.RelAnnotation, EventID: "$a", Key: "👍"},
	}}}
	// Newest first, as /messages gives them backwards.
	page := []*event.Event{
		edit("$e3", "$a", "@bob:x", 30, "by someone else"),
		edit("$e2", "$a", "@alice:x", 20, "latest"),
		reaction,
		edit("$e1", "$a", "@alice:x", 10, "first edit"),
		msg("$a", "@alice:x", 1, &event.MessageEventContent{MsgType: event.MsgText, Body: "original"}),
	}
	edits, reactions := historyChanges(page)
	if got := edits["$a"]; got == nil || got.Body != "latest" {
		t.Errorf("edit = %+v, want the sender's latest", got)
	}
	if got := reactions["$a"]; len(got) != 1 || got[0].ID != "$r" {
		t.Errorf("reactions = %+v", got)
	}
}
