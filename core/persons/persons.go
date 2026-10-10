// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package persons stores the persons of the user's address book that the
// user merged conversations into: one person, several conversations with
// them, on any network (docs/ADR/0017). It is a feature of the domain layer,
// not of the bridges: the links live in the core's database, next to the
// bridges' tables, and name conversations by their identity on their
// network, so that they can later be synchronised across the user's devices.
package persons

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"go.mau.fi/util/dbutil"
)

//go:embed schema/*.sql
var schemaFS embed.FS

// upgradeTable reads the schema from the root of a sub-filesystem, as
// localmatrix does: dbutil's WithFSPath builds backslashed paths on Windows,
// which embed.FS refuses (mautrix/go-util#46).
var upgradeTable = dbutil.BuildUpgradeTable().WithFS(mustSub(schemaFS, "schema")).Finish()

type readDirFileFS interface {
	fs.ReadDirFS
	fs.ReadFileFS
}

func mustSub(fsys fs.FS, dir string) readDirFileFS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub.(readDirFileFS)
}

// ErrNotFound is returned for a person that does not exist.
var ErrNotFound = errors.New("not found")

// idPrefix starts every person ID. The rest is random: person IDs must stay
// unique across the user's devices once the persons are synchronised.
const idPrefix = "h."

// MaxNameLength bounds a person's name, in bytes.
const MaxNameLength = 256

// Key is the identity of a conversation on its network: the bridge, the
// network's chat, and the account that reaches it (empty when every account
// of the user on that network shares the chat).
type Key struct {
	Network  string
	Chat     string
	Receiver string
}

// Person is a person and the conversations linked to them, in the order
// they were linked.
type Person struct {
	ID        string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Links     []Key
}

// Change lists what an operation changed: the persons created or modified,
// and the IDs of the persons deleted because they had no conversation left.
type Change struct {
	Updated []*Person
	Deleted []string
}

// Store holds the persons in the core's database.
type Store struct {
	db  *dbutil.Database
	now func() time.Time
}

// New returns a store over the core's database. Call Upgrade before using it.
func New(db *dbutil.Database) *Store {
	return &Store{db: db.Child("musubee_person_version", upgradeTable, nil), now: time.Now}
}

// Upgrade creates or upgrades the tables.
func (s *Store) Upgrade(ctx context.Context) error {
	return s.db.Upgrade(ctx)
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return idPrefix + hex.EncodeToString(b)
}

// ValidName reports whether name can name a person: not blank, at most
// MaxNameLength bytes.
func ValidName(name string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= MaxNameLength
}

// Create creates a person with the given conversations, moved from the
// persons they were linked to, if any. The new person comes first in the
// change.
func (s *Store) Create(ctx context.Context, name string, keys []Key) (Change, error) {
	if !ValidName(name) {
		return Change{}, fmt.Errorf("invalid person name %q", name)
	}
	if len(keys) == 0 {
		return Change{}, errors.New("a person needs at least one conversation")
	}
	var change Change
	err := s.db.DoTxn(ctx, nil, func(ctx context.Context) error {
		now := s.now().UnixMilli()
		id := newID()
		if _, err := s.db.Exec(ctx,
			`INSERT INTO musubee_person (person_id, name, created_at, updated_at) VALUES ($1, $2, $3, $3)`,
			id, name, now); err != nil {
			return err
		}
		touched, err := s.link(ctx, id, keys, now)
		if err != nil {
			return err
		}
		change, err = s.change(ctx, append([]string{id}, touched...))
		return err
	})
	return change, err
}

// Link links a conversation to a person, moving it from the person it was
// linked to, if any.
func (s *Store) Link(ctx context.Context, personID string, key Key) (Change, error) {
	var change Change
	err := s.db.DoTxn(ctx, nil, func(ctx context.Context) error {
		if _, err := s.get(ctx, personID); err != nil {
			return err
		}
		now := s.now().UnixMilli()
		touched, err := s.link(ctx, personID, []Key{key}, now)
		if err != nil {
			return err
		}
		if err = s.touch(ctx, personID, now); err != nil {
			return err
		}
		change, err = s.change(ctx, append([]string{personID}, touched...))
		return err
	})
	return change, err
}

// Unlink unlinks a conversation from its person, if it has one. A person
// left without any conversation is deleted.
func (s *Store) Unlink(ctx context.Context, key Key) (Change, error) {
	var change Change
	err := s.db.DoTxn(ctx, nil, func(ctx context.Context) error {
		owner, err := s.owner(ctx, key)
		if err != nil || owner == "" {
			return err
		}
		if _, err = s.db.Exec(ctx,
			`DELETE FROM musubee_person_link WHERE network_id=$1 AND chat_id=$2 AND receiver=$3`,
			key.Network, key.Chat, key.Receiver); err != nil {
			return err
		}
		if err = s.touch(ctx, owner, s.now().UnixMilli()); err != nil {
			return err
		}
		change, err = s.change(ctx, []string{owner})
		return err
	})
	return change, err
}

// Rename renames a person.
func (s *Store) Rename(ctx context.Context, personID, name string) (*Person, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("invalid person name %q", name)
	}
	var person *Person
	err := s.db.DoTxn(ctx, nil, func(ctx context.Context) error {
		res, err := s.db.Exec(ctx, `UPDATE musubee_person SET name=$2, updated_at=$3 WHERE person_id=$1`,
			personID, name, s.now().UnixMilli())
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return fmt.Errorf("person %s: %w", personID, ErrNotFound)
		}
		person, err = s.get(ctx, personID)
		return err
	})
	return person, err
}

// Delete deletes a person and their links, and returns the person as they
// were. The conversations stay.
func (s *Store) Delete(ctx context.Context, personID string) (*Person, error) {
	var person *Person
	err := s.db.DoTxn(ctx, nil, func(ctx context.Context) error {
		var err error
		if person, err = s.get(ctx, personID); err != nil {
			return err
		}
		_, err = s.db.Exec(ctx, `DELETE FROM musubee_person WHERE person_id=$1`, personID)
		return err
	})
	return person, err
}

// Get returns a person.
func (s *Store) Get(ctx context.Context, personID string) (*Person, error) {
	return s.get(ctx, personID)
}

// List returns every person, oldest first.
func (s *Store) List(ctx context.Context) ([]*Person, error) {
	rows, err := s.db.Query(ctx,
		`SELECT person_id, name, created_at, updated_at FROM musubee_person ORDER BY created_at, rowid`)
	if err != nil {
		return nil, err
	}
	var people []*Person
	index := map[string]*Person{}
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		people = append(people, p)
		index[p.ID] = p
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	links, err := s.db.Query(ctx,
		`SELECT person_id, network_id, chat_id, receiver FROM musubee_person_link ORDER BY linked_at, rowid`)
	if err != nil {
		return nil, err
	}
	defer links.Close()
	for links.Next() {
		var personID string
		var key Key
		if err = links.Scan(&personID, &key.Network, &key.Chat, &key.Receiver); err != nil {
			return nil, err
		}
		if p := index[personID]; p != nil {
			p.Links = append(p.Links, key)
		}
	}
	return people, links.Err()
}

// Owners returns the person of every linked conversation.
func (s *Store) Owners(ctx context.Context) (map[Key]string, error) {
	rows, err := s.db.Query(ctx, `SELECT network_id, chat_id, receiver, person_id FROM musubee_person_link`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	owners := map[Key]string{}
	for rows.Next() {
		var key Key
		var personID string
		if err = rows.Scan(&key.Network, &key.Chat, &key.Receiver, &personID); err != nil {
			return nil, err
		}
		owners[key] = personID
	}
	return owners, rows.Err()
}

// Owner returns the ID of the person a conversation is linked to, or "".
func (s *Store) Owner(ctx context.Context, key Key) (string, error) {
	return s.owner(ctx, key)
}

func (s *Store) owner(ctx context.Context, key Key) (string, error) {
	var personID string
	err := s.db.QueryRow(ctx,
		`SELECT person_id FROM musubee_person_link WHERE network_id=$1 AND chat_id=$2 AND receiver=$3`,
		key.Network, key.Chat, key.Receiver).Scan(&personID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return personID, err
}

// link links the keys to a person and returns the other persons it took
// them from.
func (s *Store) link(ctx context.Context, personID string, keys []Key, now int64) ([]string, error) {
	var touched []string
	for _, key := range keys {
		owner, err := s.owner(ctx, key)
		if err != nil {
			return nil, err
		}
		if owner == personID {
			continue
		}
		if owner != "" && !slices.Contains(touched, owner) {
			touched = append(touched, owner)
		}
		if _, err = s.db.Exec(ctx,
			`INSERT INTO musubee_person_link (network_id, chat_id, receiver, person_id, linked_at)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (network_id, chat_id, receiver) DO UPDATE SET person_id=excluded.person_id, linked_at=excluded.linked_at`,
			key.Network, key.Chat, key.Receiver, personID, now); err != nil {
			return nil, err
		}
	}
	for _, owner := range touched {
		if err := s.touch(ctx, owner, now); err != nil {
			return nil, err
		}
	}
	return touched, nil
}

func (s *Store) touch(ctx context.Context, personID string, now int64) error {
	_, err := s.db.Exec(ctx, `UPDATE musubee_person SET updated_at=$2 WHERE person_id=$1`, personID, now)
	return err
}

// change reads the persons an operation touched, and deletes those left
// without any conversation.
func (s *Store) change(ctx context.Context, ids []string) (Change, error) {
	var change Change
	for _, id := range ids {
		p, err := s.get(ctx, id)
		if err != nil {
			return change, err
		}
		if len(p.Links) > 0 {
			change.Updated = append(change.Updated, p)
			continue
		}
		if _, err = s.db.Exec(ctx, `DELETE FROM musubee_person WHERE person_id=$1`, id); err != nil {
			return change, err
		}
		change.Deleted = append(change.Deleted, id)
	}
	return change, nil
}

func scanPerson(row dbutil.Scannable) (*Person, error) {
	var p Person
	var created, updated int64
	if err := row.Scan(&p.ID, &p.Name, &created, &updated); err != nil {
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = time.UnixMilli(created), time.UnixMilli(updated)
	return &p, nil
}

func (s *Store) get(ctx context.Context, personID string) (*Person, error) {
	p, err := scanPerson(s.db.QueryRow(ctx,
		`SELECT person_id, name, created_at, updated_at FROM musubee_person WHERE person_id=$1`, personID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("person %s: %w", personID, ErrNotFound)
	} else if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx,
		`SELECT network_id, chat_id, receiver FROM musubee_person_link WHERE person_id=$1 ORDER BY linked_at, rowid`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key Key
		if err = rows.Scan(&key.Network, &key.Chat, &key.Receiver); err != nil {
			return nil, err
		}
		p.Links = append(p.Links, key)
	}
	return p, rows.Err()
}
