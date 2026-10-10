// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package persons

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/quentinemusee/musubee/core/storage/sqlite"
)

var (
	alice1 = Key{Network: "telegram", Chat: "100", Receiver: "me"}
	alice2 = Key{Network: "signal", Chat: "+33600000000", Receiver: "me"}
	bob    = Key{Network: "telegram", Chat: "200", Receiver: "me"}
	shared = Key{Network: "echo", Chat: "group", Receiver: ""}
)

func open(t *testing.T, path string) *Store {
	t.Helper()
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := New(db)
	if err := s.Upgrade(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func newStore(t *testing.T) *Store {
	t.Helper()
	return open(t, filepath.Join(t.TempDir(), "core.db"))
}

func ids(change Change) (updated []string) {
	for _, p := range change.Updated {
		updated = append(updated, p.ID)
	}
	return updated
}

func TestCreateAndList(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	change, err := s.Create(ctx, "Alice", []Key{alice1, alice2, alice1})
	if err != nil {
		t.Fatal(err)
	}
	if len(change.Updated) != 1 || len(change.Deleted) != 0 {
		t.Fatalf("change = %+v", change)
	}
	alice := change.Updated[0]
	if !strings.HasPrefix(alice.ID, idPrefix) || alice.Name != "Alice" || !slices.Equal(alice.Links, []Key{alice1, alice2}) {
		t.Fatalf("person = %+v", alice)
	}
	if _, err = s.Create(ctx, "Bob", []Key{bob}); err != nil {
		t.Fatal(err)
	}
	people, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 2 || people[0].ID != alice.ID || people[1].Name != "Bob" || !slices.Equal(people[0].Links, alice.Links) {
		t.Fatalf("list = %+v", people)
	}
	owners, err := s.Owners(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(owners) != 3 || owners[alice2] != alice.ID || owners[bob] != people[1].ID {
		t.Fatalf("owners = %v", owners)
	}
}

func TestCreateRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	for _, name := range []string{"", "   ", strings.Repeat("x", MaxNameLength+1)} {
		if _, err := s.Create(ctx, name, []Key{alice1}); err == nil {
			t.Errorf("name %q accepted", name)
		}
	}
	if _, err := s.Create(ctx, "Nobody", nil); err == nil {
		t.Error("a person without conversations was created")
	}
	if people, _ := s.List(ctx); len(people) != 0 {
		t.Fatalf("persons after failures: %+v", people)
	}
}

// A conversation belongs to one person at most: linking it elsewhere moves
// it, and a person left with nothing is deleted.
func TestLinkMovesAndDeletesEmptyPersons(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	a, _ := s.Create(ctx, "Alice", []Key{alice1})
	b, _ := s.Create(ctx, "Alice (Signal)", []Key{alice2})
	alice, other := a.Updated[0].ID, b.Updated[0].ID

	change, err := s.Link(ctx, alice, alice2)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids(change), []string{alice}) || !slices.Equal(change.Deleted, []string{other}) {
		t.Fatalf("change = %+v", change)
	}
	if !slices.Equal(change.Updated[0].Links, []Key{alice1, alice2}) {
		t.Fatalf("links = %v", change.Updated[0].Links)
	}
	if _, err = s.Get(ctx, other); !errors.Is(err, ErrNotFound) {
		t.Fatalf("emptied person: %v", err)
	}
	// Linking again changes nothing but the person's modification time.
	if change, err = s.Link(ctx, alice, alice2); err != nil || len(change.Deleted) != 0 || len(change.Updated[0].Links) != 2 {
		t.Fatalf("relink: %+v, %v", change, err)
	}
	if _, err = s.Link(ctx, "h.nope", bob); !errors.Is(err, ErrNotFound) {
		t.Fatalf("link to a missing person: %v", err)
	}
	if owner, _ := s.Owner(ctx, bob); owner != "" {
		t.Fatalf("bob was linked to %q", owner)
	}
}

func TestCreateTakesConversationsFromOtherPersons(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	first, _ := s.Create(ctx, "Alice", []Key{alice1, shared})
	second, _ := s.Create(ctx, "Bob", []Key{bob})
	change, err := s.Create(ctx, "Everyone", []Key{alice1, bob})
	if err != nil {
		t.Fatal(err)
	}
	everyone, alice, bobID := change.Updated[0].ID, first.Updated[0].ID, second.Updated[0].ID
	if !slices.Equal(ids(change), []string{everyone, alice}) || !slices.Equal(change.Deleted, []string{bobID}) {
		t.Fatalf("change = %+v", change)
	}
	if !slices.Equal(change.Updated[1].Links, []Key{shared}) {
		t.Fatalf("alice keeps %v", change.Updated[1].Links)
	}
}

func TestUnlinkRenameDelete(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	created, _ := s.Create(ctx, "Alice", []Key{alice1, alice2})
	alice := created.Updated[0].ID

	change, err := s.Unlink(ctx, alice1)
	if err != nil || !slices.Equal(ids(change), []string{alice}) || !slices.Equal(change.Updated[0].Links, []Key{alice2}) {
		t.Fatalf("unlink: %+v, %v", change, err)
	}
	// Unlinking a conversation that has no person changes nothing.
	if change, err = s.Unlink(ctx, bob); err != nil || len(change.Updated)+len(change.Deleted) != 0 {
		t.Fatalf("unlink of an unlinked conversation: %+v, %v", change, err)
	}
	p, err := s.Rename(ctx, alice, "Alice Martin")
	if err != nil || p.Name != "Alice Martin" || !slices.Equal(p.Links, []Key{alice2}) {
		t.Fatalf("rename: %+v, %v", p, err)
	}
	if _, err = s.Rename(ctx, alice, " "); err == nil {
		t.Fatal("blank name accepted")
	}
	if _, err = s.Rename(ctx, "h.nope", "X"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename of a missing person: %v", err)
	}
	if change, err = s.Unlink(ctx, alice2); err != nil || !slices.Equal(change.Deleted, []string{alice}) {
		t.Fatalf("last unlink: %+v, %v", change, err)
	}

	created, _ = s.Create(ctx, "Bob", []Key{bob})
	deleted, err := s.Delete(ctx, created.Updated[0].ID)
	if err != nil || !slices.Equal(deleted.Links, []Key{bob}) {
		t.Fatalf("delete: %+v, %v", deleted, err)
	}
	if owner, _ := s.Owner(ctx, bob); owner != "" {
		t.Fatalf("the links outlived their person: %q", owner)
	}
	if _, err = s.Delete(ctx, created.Updated[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestPersonsSurviveReopening(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "core.db")
	created, err := open(t, path).Create(ctx, "Alice", []Key{alice1, shared})
	if err != nil {
		t.Fatal(err)
	}
	people, err := open(t, path).List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 1 || people[0].ID != created.Updated[0].ID || !slices.Equal(people[0].Links, []Key{alice1, shared}) {
		t.Fatalf("after reopening: %+v", people)
	}
}
