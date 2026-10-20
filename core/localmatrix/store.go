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
	"slices"
	"strings"
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

// RoomActivity is a room with its latest message, to rank the rooms like a
// conversation list does.
type RoomActivity struct {
	Room *Room
	// LastMessage is the room's last stored message event (m.room.message,
	// not an edit), or nil if it has none.
	LastMessage *event.Event
	// At is when LastMessage was sent, or when the room was created, in
	// milliseconds since the Unix epoch.
	At int64

	lastOrder int64
}

// Cursor returns the position after this room in a ranking by activity.
func (ra *RoomActivity) Cursor() ActivityCursor {
	return ActivityCursor{At: ra.At, RoomID: ra.Room.ID}
}

// ActivityCursor is a position in the rooms ranked by activity: the rooms
// after it are less recent, or as recent with a smaller room ID.
type ActivityCursor struct {
	At     int64
	RoomID id.RoomID
}

// ReadState is the device user's read state of a room.
type ReadState struct {
	// Unread counts the messages of others stored after the user's read
	// position, up to MaxUnread.
	Unread int
	// MarkedUnread is set while the user has marked the room as unread.
	MarkedUnread bool
}

// MaxUnread bounds ReadState.Unread, so that counting stays cheap in rooms
// with a long unread history.
const MaxUnread = 1000

// Relations is what the other events of a room say about a message.
type Relations struct {
	// Edit is the latest edit of the message by its own sender (an
	// m.room.message whose m.relates_to is an m.replace of it, with the new
	// content in m.new_content), or nil. Edits by others do not count, as
	// in Matrix.
	Edit *event.Event
	// Reactions are the message's reactions (m.reaction events), one per
	// key, in the order of each key's first reaction.
	Reactions []Reaction
}

// Reaction is a key that people reacted with to a message, usually an
// emoji.
type Reaction struct {
	Key string
	// Senders are the users who reacted with it, each once, in the order
	// they did.
	Senders []id.UserID
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

// messageFilter keeps the messages of a room shown as such: m.room.message
// events that are not edits of another (those change the message they
// edit).
const messageFilter = `event_type='m.room.message' AND state_key IS NULL AND rel_type IS NOT 'm.replace'`

// activityQuery ranks the rooms by the time of their latest message (the
// last one stored), or their creation time without messages. $1 and $2 are
// the cursor (0 and "" for the start), $3 the limit (-1: no limit).
const activityQuery = `
WITH latest AS (
	SELECT r.room_id, r.bridge_id, r.creator, r.is_direct, r.created_at,
		(SELECT MAX(e.stream_order) FROM local_event e
		 WHERE e.room_id=r.room_id AND e.event_type='m.room.message' AND e.state_key IS NULL
		   AND e.rel_type IS NOT 'm.replace') AS last_order
	FROM local_room r
), ranked AS (
	SELECT l.*, COALESCE(e.timestamp, l.created_at) AS activity
	FROM latest l LEFT JOIN local_event e ON e.stream_order=l.last_order
)
SELECT room_id, bridge_id, creator, is_direct, created_at, activity, COALESCE(last_order, 0) FROM ranked
WHERE ($1=0 AND $2='') OR activity<$1 OR (activity=$1 AND room_id<$2)
ORDER BY activity DESC, room_id DESC
LIMIT $3`

// listRoomsByActivity returns the rooms ranked by activity, most recent
// first, after the cursor (nil for the start); limit <= 0 returns them all.
func (s *store) listRoomsByActivity(ctx context.Context, after *ActivityCursor, limit int) ([]*RoomActivity, error) {
	var at int64
	var roomID id.RoomID
	if after != nil {
		at, roomID = after.At, after.RoomID
	}
	if limit <= 0 {
		limit = -1
	}
	rows, err := s.db.Query(ctx, activityQuery, at, roomID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rooms []*RoomActivity
	for rows.Next() {
		var ra RoomActivity
		var room Room
		var createdAt int64
		if err = rows.Scan(&room.ID, &room.BridgeID, &room.Creator, &room.IsDirect, &createdAt, &ra.At, &ra.lastOrder); err != nil {
			return nil, err
		}
		room.CreatedAt = time.UnixMilli(createdAt)
		ra.Room = &room
		rooms = append(rooms, &ra)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for _, ra := range rooms {
		if err = s.fillLastMessage(ctx, ra); err != nil {
			return nil, err
		}
	}
	return rooms, nil
}

// roomActivity returns the activity of one room, or an error wrapping
// ErrNotFound.
func (s *store) roomActivity(ctx context.Context, roomID id.RoomID) (*RoomActivity, error) {
	room, err := s.getRoom(ctx, roomID)
	if err != nil {
		return nil, err
	}
	ra := RoomActivity{Room: room, At: room.CreatedAt.UnixMilli()}
	err = s.db.QueryRow(ctx,
		`SELECT COALESCE(MAX(stream_order), 0) FROM local_event WHERE room_id=$1 AND `+messageFilter,
		roomID).Scan(&ra.lastOrder)
	if err != nil {
		return nil, err
	}
	if err = s.fillLastMessage(ctx, &ra); err != nil {
		return nil, err
	}
	if ra.LastMessage != nil {
		ra.At = ra.LastMessage.Timestamp
	}
	return &ra, nil
}

func (s *store) fillLastMessage(ctx context.Context, ra *RoomActivity) error {
	if ra.lastOrder == 0 {
		return nil
	}
	evt, err := scanEvent(s.db.QueryRow(ctx, `SELECT `+eventColumns+` FROM local_event WHERE stream_order=$1`, ra.lastOrder))
	if err != nil {
		return fmt.Errorf("reading the latest message of %s: %w", ra.Room.ID, err)
	}
	ra.LastMessage = evt
	return nil
}

// relation is the part of an event's content that says what it relates to.
type relation struct {
	RelatesTo *struct {
		Type    event.RelationType `json:"rel_type"`
		EventID id.EventID         `json:"event_id"`
	} `json:"m.relates_to"`
}

// relationOf returns the type and target of an event's relation (m.replace
// for an edit, m.annotation for a reaction), or NULLs without one. A reply
// (m.in_reply_to) has no relation type: it is a message of its own.
func relationOf(evt *event.Event, content []byte) (relType, target sql.NullString) {
	var rel relation
	if evt.StateKey != nil || json.Unmarshal(content, &rel) != nil || rel.RelatesTo == nil ||
		rel.RelatesTo.Type == "" || rel.RelatesTo.EventID == "" {
		return relType, target
	}
	relType = sql.NullString{String: string(rel.RelatesTo.Type), Valid: true}
	target = sql.NullString{String: string(rel.RelatesTo.EventID), Valid: true}
	return relType, target
}

// insertEvent stores an event and, for state events, makes it the current
// state. It fills evt.Unsigned.BeeperHSOrder.
func (s *store) insertEvent(ctx context.Context, evt *event.Event) error {
	content, err := json.Marshal(&evt.Content)
	if err != nil {
		return fmt.Errorf("encoding event content: %w", err)
	}
	relType, target := relationOf(evt, content)
	return s.db.DoTxn(ctx, nil, func(ctx context.Context) error {
		res, err := s.db.Exec(ctx,
			`INSERT INTO local_event (event_id, room_id, sender, event_type, state_key, content, timestamp, rel_type, relates_to)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			evt.ID, evt.RoomID, evt.Sender, evt.Type.Type, evt.StateKey, string(content), evt.Timestamp, relType, target)
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
// is redacted, so that deleted messages do not stay on the device: of a
// message, its edits too, which repeat its text. Only the relation stays
// (as Matrix's MSC3389 proposes), to tell what a redacted edit or reaction
// was about.
func (s *store) redact(ctx context.Context, roomID id.RoomID, redaction, eventID id.EventID) error {
	if eventID == "" {
		return nil
	}
	_, err := s.db.Exec(ctx,
		`UPDATE local_event
		 SET content=CASE WHEN rel_type IS NULL THEN '{}'
		                  ELSE json_object('m.relates_to', json_object('rel_type', rel_type, 'event_id', relates_to)) END,
		     redacted_by=$3
		 WHERE room_id=$1 AND state_key IS NULL
		   AND (event_id=$2 OR (relates_to=$2 AND rel_type='m.replace'))`,
		roomID, eventID, redaction)
	return err
}

const eventColumns = `stream_order, event_id, room_id, sender, event_type, state_key, content, timestamp, redacted_by`

// scanEvent reads an event. A redacted one has its content erased and
// Unsigned.RedactedBecause set to a redaction that only has its ID.
func scanEvent(row dbutil.Scannable) (*event.Event, error) {
	var evt event.Event
	var eventType, content string
	var stateKey, redactedBy sql.NullString
	err := row.Scan(&evt.Unsigned.BeeperHSOrder, &evt.ID, &evt.RoomID, &evt.Sender, &eventType, &stateKey, &content, &evt.Timestamp, &redactedBy)
	if err != nil {
		return nil, err
	}
	evt.Type = event.Type{Type: eventType, Class: event.MessageEventType}
	if stateKey.Valid {
		evt.StateKey = &stateKey.String
		evt.Type.Class = event.StateEventType
	}
	if redactedBy.Valid {
		evt.Unsigned.RedactedBecause = &event.Event{ID: id.EventID(redactedBy.String), Type: event.EventRedaction}
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

// messagesBefore returns the message events (m.room.message, without the
// edits) of a room stored before the given stream order, or the latest ones
// when before is 0, newest first.
func (s *store) messagesBefore(ctx context.Context, roomID id.RoomID, before int64, limit int) ([]*event.Event, error) {
	rows, err := s.db.Query(ctx,
		`SELECT `+eventColumns+` FROM local_event
		 WHERE room_id=$1 AND `+messageFilter+` AND ($2=0 OR stream_order<$2)
		 ORDER BY stream_order DESC LIMIT $3`,
		roomID, before, limit)
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

// relations returns the relations of messages of a room, by their event
// ID, leaving out what was redacted. A message without any has no entry.
func (s *store) relations(ctx context.Context, roomID id.RoomID, messages []*event.Event) (map[id.EventID]*Relations, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	senders := make(map[id.EventID]id.UserID, len(messages))
	args := make([]any, 0, len(messages)+1)
	args = append(args, roomID)
	placeholders := make([]string, 0, len(messages))
	for _, msg := range messages {
		if _, seen := senders[msg.ID]; seen {
			continue
		}
		senders[msg.ID] = msg.Sender
		args = append(args, msg.ID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	rows, err := s.db.Query(ctx,
		`SELECT `+eventColumns+` FROM local_event
		 WHERE room_id=$1 AND relates_to IN (`+strings.Join(placeholders, ",")+`)
		   AND rel_type IN ('m.replace', 'm.annotation') AND redacted_by IS NULL
		 ORDER BY stream_order`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[id.EventID]*Relations)
	of := func(target id.EventID) *Relations {
		rel := result[target]
		if rel == nil {
			rel = &Relations{}
			result[target] = rel
		}
		return rel
	}
	for rows.Next() {
		evt, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		switch evt.Type {
		case event.EventMessage:
			content := evt.Content.AsMessage()
			target := content.RelatesTo.GetReplaceID()
			if target != "" && evt.Sender == senders[target] && content.NewContent != nil {
				of(target).Edit = evt
			}
		case event.EventReaction:
			annotation := evt.Content.AsReaction().RelatesTo
			if annotation.Type == event.RelAnnotation && annotation.Key != "" {
				of(annotation.EventID).addReaction(annotation.Key, evt.Sender)
			}
		}
	}
	return result, rows.Err()
}

func (rel *Relations) addReaction(key string, sender id.UserID) {
	for i := range rel.Reactions {
		if rel.Reactions[i].Key == key {
			if !slices.Contains(rel.Reactions[i].Senders, sender) {
				rel.Reactions[i].Senders = append(rel.Reactions[i].Senders, sender)
			}
			return
		}
	}
	rel.Reactions = append(rel.Reactions, Reaction{Key: key, Senders: []id.UserID{sender}})
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

// readOrder returns the stream order a read receipt points to: the event's
// own when it is stored in the room, otherwise the last event of the room
// sent at or before ts (receipts may name events this storage never had),
// or 0.
func (s *store) readOrder(ctx context.Context, roomID id.RoomID, eventID id.EventID, ts time.Time) (int64, error) {
	var order int64
	err := s.db.QueryRow(ctx,
		`SELECT stream_order FROM local_event WHERE room_id=$1 AND event_id=$2`, roomID, eventID).Scan(&order)
	if !errors.Is(err, sql.ErrNoRows) {
		return order, err
	}
	err = s.db.QueryRow(ctx,
		`SELECT COALESCE(MAX(stream_order), 0) FROM local_event WHERE room_id=$1 AND timestamp<=$2`,
		roomID, ts.UnixMilli()).Scan(&order)
	return order, err
}

// advanceReadMarker moves the user's read position in a room to order, if
// that is further than where it is. It reports whether it moved.
func (s *store) advanceReadMarker(ctx context.Context, roomID id.RoomID, order int64, at time.Time) (bool, error) {
	res, err := s.db.Exec(ctx,
		`INSERT INTO local_read_marker (room_id, stream_order, updated_at) VALUES ($1, $2, $3)
		 ON CONFLICT (room_id) DO UPDATE SET stream_order=excluded.stream_order, updated_at=excluded.updated_at
		 WHERE excluded.stream_order > local_read_marker.stream_order`,
		roomID, order, at.UnixMilli())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// unreadQuery counts, up to a limit ($3), the messages of others stored
// up to a stream order ($4) after the user's read position, which is the
// read marker or the user's own last message, whichever is later: writing
// in a room implies reading it. Deleted messages do not count.
const unreadQuery = `
SELECT COUNT(*) FROM (
	SELECT 1 FROM local_event
	WHERE room_id=$1 AND ` + messageFilter + ` AND redacted_by IS NULL AND sender<>$2
	  AND stream_order <= $4
	  AND stream_order > MAX(
		COALESCE((SELECT stream_order FROM local_read_marker WHERE room_id=$1), 0),
		COALESCE((SELECT MAX(stream_order) FROM local_event
		          WHERE room_id=$1 AND event_type='m.room.message' AND state_key IS NULL AND sender=$2), 0))
	LIMIT $3
)`

// readState returns the user's read state of a room, counting the messages
// stored up to upTo.
func (s *store) readState(ctx context.Context, roomID id.RoomID, userID id.UserID, upTo int64) (*ReadState, error) {
	var st ReadState
	if err := s.db.QueryRow(ctx, unreadQuery, roomID, userID, MaxUnread, upTo).Scan(&st.Unread); err != nil {
		return nil, err
	}
	var marked event.MarkedUnreadEventContent
	if _, err := s.getRoomAccountData(ctx, userID, roomID, accountDataUnread, &marked); err != nil {
		return nil, err
	}
	st.MarkedUnread = marked.Unread
	return &st, nil
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
