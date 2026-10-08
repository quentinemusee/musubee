// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package localmatrix

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

//go:embed schema/*.sql
var schemaFS embed.FS

// upgradeTable reads the schema from the root of a sub-filesystem:
// dbutil's WithFSPath joins paths with filepath.Join, which produces
// backslashes on Windows, and embed.FS only accepts forward slashes.
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

// ErrNotFound is returned when a room, event or media does not exist.
var ErrNotFound = errors.New("not found")

// store holds the rooms, events and related data in SQLite.
type store struct {
	db *dbutil.Database
}

func newStore(db *dbutil.Database) *store {
	return &store{db: db.Child("local_version", upgradeTable, nil)}
}

func (s *store) upgrade(ctx context.Context) error {
	return s.db.Upgrade(ctx)
}

// Room is a stored room.
type Room struct {
	ID        id.RoomID
	BridgeID  string
	Creator   id.UserID
	IsDirect  bool
	CreatedAt time.Time
}

// MessageStatus is the delivery status of a message sent by the user.
type MessageStatus struct {
	RoomID  id.RoomID
	EventID id.EventID
	Status  event.MessageStatus
	Reason  event.MessageStatusReason
	// Message is a human-readable explanation, shown to the user on failure.
	Message string
	// NewEventID is set when the bridge replaced the event ID.
	NewEventID id.EventID
	UpdatedAt  time.Time
}

func (s *store) createRoom(ctx context.Context, room *Room) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO local_room (room_id, bridge_id, creator, is_direct, created_at) VALUES ($1, $2, $3, $4, $5)`,
		room.ID, room.BridgeID, room.Creator, room.IsDirect, room.CreatedAt.UnixMilli())
	return err
}

func (s *store) deleteRoom(ctx context.Context, roomID id.RoomID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM local_room WHERE room_id=$1`, roomID)
	return err
}

func scanRoom(row dbutil.Scannable) (*Room, error) {
	var room Room
	var createdAt int64
	err := row.Scan(&room.ID, &room.BridgeID, &room.Creator, &room.IsDirect, &createdAt)
	if err != nil {
		return nil, err
	}
	room.CreatedAt = time.UnixMilli(createdAt)
	return &room, nil
}

func (s *store) getRoom(ctx context.Context, roomID id.RoomID) (*Room, error) {
	room, err := scanRoom(s.db.QueryRow(ctx,
		`SELECT room_id, bridge_id, creator, is_direct, created_at FROM local_room WHERE room_id=$1`, roomID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("room %s: %w", roomID, ErrNotFound)
	}
	return room, err
}

func (s *store) listRooms(ctx context.Context) ([]*Room, error) {
	rows, err := s.db.Query(ctx,
		`SELECT room_id, bridge_id, creator, is_direct, created_at FROM local_room ORDER BY created_at, room_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rooms []*Room
	for rows.Next() {
		room, err := scanRoom(rows)
		if err != nil {
			return nil, err
		}
		rooms = append(rooms, room)
	}
	return rooms, rows.Err()
}

// insertEvent stores an event and, for state events, makes it the current
// state. It fills evt.Unsigned.BeeperHSOrder.
func (s *store) insertEvent(ctx context.Context, evt *event.Event) error {
	content, err := json.Marshal(&evt.Content)
	if err != nil {
		return fmt.Errorf("encoding event content: %w", err)
	}
	return s.db.DoTxn(ctx, nil, func(ctx context.Context) error {
		res, err := s.db.Exec(ctx,
			`INSERT INTO local_event (event_id, room_id, sender, event_type, state_key, content, timestamp)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			evt.ID, evt.RoomID, evt.Sender, evt.Type.Type, evt.StateKey, string(content), evt.Timestamp)
		if err != nil {
			return err
		}
		if evt.Unsigned.BeeperHSOrder, err = res.LastInsertId(); err != nil {
			return err
		}
		if evt.StateKey == nil {
			return nil
		}
		_, err = s.db.Exec(ctx,
			`INSERT INTO local_state (room_id, event_type, state_key, event_id) VALUES ($1, $2, $3, $4)
			 ON CONFLICT (room_id, event_type, state_key) DO UPDATE SET event_id=excluded.event_id`,
			evt.RoomID, evt.Type.Type, *evt.StateKey, evt.ID)
		return err
	})
}

// redact erases the content of an event, as a homeserver does when an event
// is redacted, so that deleted messages do not stay on the device.
func (s *store) redact(ctx context.Context, roomID id.RoomID, eventID id.EventID) error {
	if eventID == "" {
		return nil
	}
	_, err := s.db.Exec(ctx, `UPDATE local_event SET content='{}' WHERE room_id=$1 AND event_id=$2`, roomID, eventID)
	return err
}

const eventColumns = `stream_order, event_id, room_id, sender, event_type, state_key, content, timestamp`

func scanEvent(row dbutil.Scannable) (*event.Event, error) {
	var evt event.Event
	var eventType, content string
	var stateKey sql.NullString
	err := row.Scan(&evt.Unsigned.BeeperHSOrder, &evt.ID, &evt.RoomID, &evt.Sender, &eventType, &stateKey, &content, &evt.Timestamp)
	if err != nil {
		return nil, err
	}
	evt.Type = event.Type{Type: eventType, Class: event.MessageEventType}
	if stateKey.Valid {
		evt.StateKey = &stateKey.String
		evt.Type.Class = event.StateEventType
	}
	if err = json.Unmarshal([]byte(content), &evt.Content); err != nil {
		return nil, fmt.Errorf("decoding content of %s: %w", evt.ID, err)
	}
	// Parsing fails for event types unknown to mautrix; the raw content is
	// still available in that case.
	_ = evt.Content.ParseRaw(evt.Type)
	return &evt, nil
}

func (s *store) getEvent(ctx context.Context, roomID id.RoomID, eventID id.EventID) (*event.Event, error) {
	evt, err := scanEvent(s.db.QueryRow(ctx,
		`SELECT `+eventColumns+` FROM local_event WHERE room_id=$1 AND event_id=$2`, roomID, eventID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("event %s: %w", eventID, ErrNotFound)
	}
	return evt, err
}

// timeline returns the events of a room stored after the given stream order,
// oldest first.
func (s *store) timeline(ctx context.Context, roomID id.RoomID, after int64, limit int) ([]*event.Event, error) {
	rows, err := s.db.Query(ctx,
		`SELECT `+eventColumns+` FROM local_event WHERE room_id=$1 AND stream_order>$2 ORDER BY stream_order LIMIT $3`,
		roomID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []*event.Event
	for rows.Next() {
		evt, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, evt)
	}
	return events, rows.Err()
}

func (s *store) getState(ctx context.Context, roomID id.RoomID, eventType event.Type, stateKey string) (*event.Event, error) {
	evt, err := scanEvent(s.db.QueryRow(ctx,
		`SELECT `+eventColumns+` FROM local_event
		 WHERE event_id=(SELECT event_id FROM local_state WHERE room_id=$1 AND event_type=$2 AND state_key=$3)`,
		roomID, eventType.Type, stateKey))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return evt, err
}

func (s *store) getStateOfType(ctx context.Context, roomID id.RoomID, eventType event.Type) ([]*event.Event, error) {
	rows, err := s.db.Query(ctx,
		`SELECT `+eventColumns+` FROM local_event
		 WHERE event_id IN (SELECT event_id FROM local_state WHERE room_id=$1 AND event_type=$2)`,
		roomID, eventType.Type)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []*event.Event
	for rows.Next() {
		evt, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, evt)
	}
	return events, rows.Err()
}

func (s *store) setMessageStatus(ctx context.Context, st *MessageStatus) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO local_message_status (event_id, room_id, status, reason, message, new_event_id, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (event_id) DO UPDATE SET status=excluded.status, reason=excluded.reason,
		     message=excluded.message, new_event_id=excluded.new_event_id, updated_at=excluded.updated_at`,
		st.EventID, st.RoomID, st.Status, st.Reason, st.Message, st.NewEventID, st.UpdatedAt.UnixMilli())
	return err
}

func (s *store) getMessageStatus(ctx context.Context, eventID id.EventID) (*MessageStatus, error) {
	var st MessageStatus
	var updatedAt int64
	err := s.db.QueryRow(ctx,
		`SELECT event_id, room_id, status, reason, message, new_event_id, updated_at FROM local_message_status WHERE event_id=$1`,
		eventID).Scan(&st.EventID, &st.RoomID, &st.Status, &st.Reason, &st.Message, &st.NewEventID, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	st.UpdatedAt = time.UnixMilli(updatedAt)
	return &st, nil
}

func (s *store) setBridgeState(ctx context.Context, bridgeID, loginID string, content []byte, at time.Time) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO local_bridge_state (bridge_id, login_id, content, updated_at) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (bridge_id, login_id) DO UPDATE SET content=excluded.content, updated_at=excluded.updated_at`,
		bridgeID, loginID, string(content), at.UnixMilli())
	return err
}

// clearBridgeStates forgets the connection states of a previous process.
func (s *store) clearBridgeStates(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `DELETE FROM local_bridge_state`)
	return err
}

func (s *store) getBridgeState(ctx context.Context, bridgeID, loginID string) ([]byte, error) {
	var content string
	err := s.db.QueryRow(ctx,
		`SELECT content FROM local_bridge_state WHERE bridge_id=$1 AND login_id=$2`, bridgeID, loginID).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return []byte(content), err
}

// Profile is the global profile of a user (the local user or a ghost).
type Profile struct {
	UserID      id.UserID
	DisplayName string
	AvatarURL   id.ContentURIString
	// Extra holds the network-specific profile fields set by bridgev2.
	Extra json.RawMessage
}

func (s *store) getProfile(ctx context.Context, userID id.UserID) (*Profile, error) {
	p := Profile{UserID: userID}
	var extra string
	err := s.db.QueryRow(ctx,
		`SELECT displayname, avatar_url, extra FROM local_profile WHERE user_id=$1`, userID).
		Scan(&p.DisplayName, &p.AvatarURL, &extra)
	if errors.Is(err, sql.ErrNoRows) {
		return &p, nil
	} else if err != nil {
		return nil, err
	}
	if extra != "" {
		p.Extra = json.RawMessage(extra)
	}
	return &p, nil
}

func (s *store) setProfile(ctx context.Context, p *Profile) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO local_profile (user_id, displayname, avatar_url, extra) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (user_id) DO UPDATE SET displayname=excluded.displayname, avatar_url=excluded.avatar_url, extra=excluded.extra`,
		p.UserID, p.DisplayName, p.AvatarURL, string(p.Extra))
	return err
}

func (s *store) setRoomAccountData(ctx context.Context, userID id.UserID, roomID id.RoomID, dataType string, content any) error {
	data, err := json.Marshal(content)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx,
		`INSERT INTO local_room_account_data (user_id, room_id, data_type, content) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (user_id, room_id, data_type) DO UPDATE SET content=excluded.content`,
		userID, roomID, dataType, string(data))
	return err
}

func (s *store) getRoomAccountData(ctx context.Context, userID id.UserID, roomID id.RoomID, dataType string, into any) (bool, error) {
	var content string
	err := s.db.QueryRow(ctx,
		`SELECT content FROM local_room_account_data WHERE user_id=$1 AND room_id=$2 AND data_type=$3`,
		userID, roomID, dataType).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(content), into)
}

func (s *store) putMedia(ctx context.Context, mediaID, mimeType, fileName string, data []byte) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO local_media (media_id, mime_type, file_name, data, created_at) VALUES ($1, $2, $3, $4, $5)`,
		mediaID, mimeType, fileName, data, time.Now().UnixMilli())
	return err
}

func (s *store) getMedia(ctx context.Context, mediaID string) ([]byte, error) {
	var data []byte
	err := s.db.QueryRow(ctx, `SELECT data FROM local_media WHERE media_id=$1`, mediaID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("media %s: %w", mediaID, ErrNotFound)
	}
	return data, err
}
