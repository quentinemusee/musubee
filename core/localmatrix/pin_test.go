// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package localmatrix_test

import (
	"slices"
	"testing"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/localmatrix"
)

// TestPin checks a room's pin (docs/ADR/0052): the user's favourite tag,
// set here or by the network, kept with the user's other tags, published
// as a read state update when it changes, and listed by PinnedRooms.
func TestPin(t *testing.T) {
	server := newServer(t, localmatrix.Options{})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	alice := createDM(t, server, conn, "alice")
	bob := createDM(t, server, conn, "bob")
	user, _, err := conn.NewUserIntent(ctx, server.UserID(), "")
	if err != nil {
		t.Fatal(err)
	}
	updates, unsubscribe := server.Subscribe(1000)
	defer unsubscribe()
	published := func() int {
		n := 0
		for len(updates) > 0 {
			if update := <-updates; update.ReadState != "" {
				n++
			}
		}
		return n
	}
	pinned := func(what string, want ...id.RoomID) {
		t.Helper()
		rooms, err := server.PinnedRooms(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var got []id.RoomID
		for _, room := range rooms {
			got = append(got, room.Room.ID)
		}
		// Both rooms may have the same activity: compare as sets.
		slices.Sort(got)
		want = slices.Sorted(slices.Values(want))
		if !slices.Equal(got, want) {
			t.Fatalf("%s: pinned %v, want %v", what, got, want)
		}
		for _, roomID := range []id.RoomID{alice, bob} {
			st, err := server.ReadState(ctx, roomID)
			if err != nil {
				t.Fatal(err)
			}
			if wanted := slices.Contains(want, roomID); st.Pinned != wanted {
				t.Errorf("%s: %s pinned = %v, want %v", what, roomID, st.Pinned, wanted)
			}
		}
	}

	pinned("new rooms")
	// A low priority tag set in another app stays, and is no pin.
	if err = user.TagRoom(ctx, alice, event.RoomTagLowPriority, true); err != nil {
		t.Fatal(err)
	}
	pinned("low priority")
	if n := published(); n != 0 {
		t.Errorf("a low priority tag published %d updates", n)
	}

	if err = server.Pin(ctx, alice, true); err != nil {
		t.Fatal(err)
	}
	pinned("Alice pinned", alice)
	if err = server.Pin(ctx, alice, true); err != nil {
		t.Fatal(err)
	}
	if n := published(); n != 1 {
		t.Errorf("pinning twice published %d updates, want 1", n)
	}

	// Bob's room pinned on another device.
	if err = user.TagRoom(ctx, bob, event.RoomTagFavourite, true); err != nil {
		t.Fatal(err)
	}
	pinned("both pinned", bob, alice)
	if n := published(); n != 1 {
		t.Errorf("a pin from the network published %d updates, want 1", n)
	}

	if err = server.Pin(ctx, alice, false); err != nil {
		t.Fatal(err)
	}
	pinned("Alice unpinned", bob)
}
