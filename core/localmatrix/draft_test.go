// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package localmatrix_test

import (
	"errors"
	"testing"

	"github.com/quentinemusee/musubee/core/localmatrix"
)

// TestDraft keeps, replaces and forgets the text left unsent in a room
// (docs/ADR/0057).
func TestDraft(t *testing.T) {
	server := newServer(t, localmatrix.Options{})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	roomID := createDM(t, server, conn, "alice")
	expect := func(want string) {
		t.Helper()
		if got, err := server.Draft(ctx, roomID); err != nil || got != want {
			t.Errorf("Draft = %q, %v; want %q", got, err, want)
		}
	}
	expect("")
	for _, text := range []string{"half a thought", "half a thought, finished", ""} {
		if err := server.SetDraft(ctx, roomID, text); err != nil {
			t.Fatal(err)
		}
		expect(text)
	}
	if err := server.SetDraft(ctx, "!unknown:musubee.local", "x"); !errors.Is(err, localmatrix.ErrNotFound) {
		t.Errorf("SetDraft in an unknown room: %v, want ErrNotFound", err)
	}
}
