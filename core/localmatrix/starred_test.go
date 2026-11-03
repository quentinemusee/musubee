// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package localmatrix_test

import (
	"testing"
	"time"

	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/localmatrix"
)

// TestStar stars and unstars messages of two rooms, listed the most
// recently starred first (docs/ADR/0065).
func TestStar(t *testing.T) {
	server := newServer(t, localmatrix.Options{})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	alice := createDM(t, server, conn, "alice")
	bob := createDM(t, server, conn, "bob")
	star := func(room string, roomID, eventID string, on, changes bool) {
		t.Helper()
		changed, err := server.Star(ctx, id.RoomID(roomID), id.EventID(eventID), on)
		if err != nil {
			t.Fatal(err)
		}
		if changed != changes {
			t.Errorf("starring %s in %s: changed = %v, want %v", eventID, room, changed, changes)
		}
	}
	star("alice", string(alice), "$a1", true, true)
	time.Sleep(2 * time.Millisecond)
	star("bob", string(bob), "$b1", true, true)
	star("bob", string(bob), "$b1", true, false)

	all, err := server.AllStarred(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].EventID != "$b1" || all[0].RoomID != bob || all[1].EventID != "$a1" {
		t.Fatalf("AllStarred = %+v, want $b1 then $a1", all)
	}
	star("alice", string(alice), "$a1", false, true)
	if inAlice, err := server.Starred(ctx, alice); err != nil || len(inAlice) != 0 {
		t.Errorf("Starred(alice) = %v, %v; want none", inAlice, err)
	}
}
