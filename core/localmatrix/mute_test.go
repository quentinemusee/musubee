// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package localmatrix_test

import (
	"testing"
	"time"

	"maunium.net/go/mautrix/event"

	"github.com/quentinemusee/musubee/core/localmatrix"
)

// TestMute checks a room's mute (docs/ADR/0050): set here or by the network
// for the user, kept as the user's account data, published as a read state
// update when it changes, and never touched by a ghost's mute.
func TestMute(t *testing.T) {
	server := newServer(t, localmatrix.Options{})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	roomID := createDM(t, server, conn, "alice")
	alice := conn.GhostIntent("alice")
	user, _, err := conn.NewUserIntent(ctx, server.UserID(), "")
	if err != nil {
		t.Fatal(err)
	}
	updates, unsubscribe := server.Subscribe(1000)
	defer unsubscribe()
	readUpdates := func() int {
		n := 0
		for len(updates) > 0 {
			if update := <-updates; update.ReadState == roomID {
				n++
			}
		}
		return n
	}
	expect := func(what string, want int64, published int) {
		t.Helper()
		st, err := server.ReadState(ctx, roomID)
		if err != nil {
			t.Fatal(err)
		}
		if st.MutedUntil != want {
			t.Errorf("%s: muted until %d, want %d", what, st.MutedUntil, want)
		}
		if n := readUpdates(); n != published {
			t.Errorf("%s: %d updates published, want %d", what, n, published)
		}
	}

	expect("new room", 0, 0)
	inAnHour := time.Now().Add(time.Hour).UnixMilli()
	if err = server.Mute(ctx, roomID, inAnHour); err != nil {
		t.Fatal(err)
	}
	expect("muted for an hour", inAnHour, 1)
	if err = server.Mute(ctx, roomID, inAnHour); err != nil {
		t.Fatal(err)
	}
	expect("muted again", inAnHour, 0)
	if err = server.Mute(ctx, roomID, localmatrix.MutedForever); err != nil {
		t.Fatal(err)
	}
	expect("muted forever", localmatrix.MutedForever, 1)

	// Unmuted in another app of the network, then muted there until
	// unmuted.
	if err = user.MuteRoom(ctx, roomID, time.Time{}); err != nil {
		t.Fatal(err)
	}
	expect("unmuted on another device", 0, 1)
	if err = user.MuteRoom(ctx, roomID, event.MutedForever); err != nil {
		t.Fatal(err)
	}
	expect("muted forever on another device", localmatrix.MutedForever, 1)

	// A ghost's mute is its own.
	if err = alice.MuteRoom(ctx, roomID, time.Time{}); err != nil {
		t.Fatal(err)
	}
	expect("Alice's mute", localmatrix.MutedForever, 0)

	if err = server.Mute(ctx, roomID, 0); err != nil {
		t.Fatal(err)
	}
	expect("unmuted", 0, 1)
}

func TestMutedUntilMS(t *testing.T) {
	at := time.UnixMilli(1_800_000_000_000)
	for _, tc := range []struct {
		until time.Time
		want  int64
	}{
		{time.Time{}, 0},
		{at, 1_800_000_000_000},
		{event.MutedForever, localmatrix.MutedForever},
	} {
		if got := localmatrix.MutedUntilMS(tc.until); got != tc.want {
			t.Errorf("MutedUntilMS(%v) = %d, want %d", tc.until, got, tc.want)
		}
	}
}
