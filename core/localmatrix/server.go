// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package localmatrix runs mautrix-go bridgev2 network connectors without a
// Matrix homeserver.
//
// bridgev2 talks to Matrix only through two interfaces, MatrixConnector and
// MatrixAPI (maunium.net/go/mautrix/bridgev2/matrixinterface.go). Instead of
// sending events to a homeserver, this package stores rooms and events in the
// local SQLite database and lets the application read them and send messages
// as the device's user. There is no network traffic on the Matrix side: the
// only remote party is the network the connector talks to (Telegram, Signal,
// ...). See docs/ADR/0009-bridgev2-in-process.md.
//
// One Server holds the storage shared by every network; each bridgev2.Bridge
// (one per network) gets its own Connector from Server.NewConnector.
package localmatrix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// DefaultServerName is the server part of the local Matrix identifiers. It
// never resolves to a real server: the ".local" name keeps the identifiers
// out of the public Matrix namespace.
const DefaultServerName = "musubee.local"

// ErrForbidden is returned when the user acts in a room they are not in.
var ErrForbidden = errors.New("forbidden")

// Options configures a Server.
type Options struct {
	// ServerName is the server part of every identifier (DefaultServerName if empty).
	ServerName string
	// UserLocalpart is the localpart of the device's user ("me" if empty).
	UserLocalpart string
	// PortalWait bounds how long SendMessage waits for a new room to become
	// a portal (DefaultPortalWait if zero; see waitForPortal).
	PortalWait time.Duration
	Log        zerolog.Logger
}

// Server stores the rooms of every network and serves the application.
type Server struct {
	store      *store
	serverName string
	userID     id.UserID
	portalWait time.Duration
	log        zerolog.Logger

	upgradeLock sync.Mutex
	upgraded    bool

	lock        sync.Mutex
	connectors  map[string]*Connector
	subscribers map[*subscriber]struct{}
}

// New creates a Server that stores its data in db. Its tables are created or
// upgraded when the first connector starts.
func New(db *dbutil.Database, opts Options) *Server {
	if opts.ServerName == "" {
		opts.ServerName = DefaultServerName
	}
	if opts.UserLocalpart == "" {
		opts.UserLocalpart = "me"
	}
	if opts.PortalWait <= 0 {
		opts.PortalWait = DefaultPortalWait
	}
	return &Server{
		store:       newStore(db),
		serverName:  opts.ServerName,
		userID:      id.NewUserID(opts.UserLocalpart, opts.ServerName),
		portalWait:  opts.PortalWait,
		log:         opts.Log,
		connectors:  make(map[string]*Connector),
		subscribers: make(map[*subscriber]struct{}),
	}
}

// UserID is the Matrix identifier of the device's user.
func (s *Server) UserID() id.UserID {
	return s.userID
}

// ServerName is the server part of the local identifiers.
func (s *Server) ServerName() string {
	return s.serverName
}

// Upgrade creates or upgrades the local tables, and forgets the bridge states
// stored by a previous process: they describe connections that no longer
// exist, and the bridges report fresh ones when they connect. Connectors call
// it on start; it only runs once.
func (s *Server) Upgrade(ctx context.Context) error {
	s.upgradeLock.Lock()
	defer s.upgradeLock.Unlock()
	if s.upgraded {
		return nil
	}
	if err := s.store.upgrade(ctx); err != nil {
		return fmt.Errorf("upgrading local Matrix tables: %w", err)
	}
	if err := s.store.clearBridgeStates(ctx); err != nil {
		return fmt.Errorf("clearing previous bridge states: %w", err)
	}
	s.upgraded = true
	return nil
}

// Bridge IDs become part of Matrix user IDs, and "_" separates them from the
// remote user ID in ghost IDs.
var bridgeIDPattern = regexp.MustCompile(`^[a-z0-9]{1,32}$`)

// NewConnector returns the Matrix connector for one bridgev2.Bridge. Pass it
// to bridgev2.NewBridge with the same bridge ID.
func (s *Server) NewConnector(bridgeID string) (*Connector, error) {
	if !bridgeIDPattern.MatchString(bridgeID) {
		return nil, fmt.Errorf("invalid bridge ID %q: use 1 to 32 lowercase letters and digits", bridgeID)
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	if _, exists := s.connectors[bridgeID]; exists {
		return nil, fmt.Errorf("a connector for bridge %q already exists", bridgeID)
	}
	if localpart, _, _ := s.userID.Parse(); localpart == bridgeID+"bot" || len(localpart) > len(bridgeID) && localpart[:len(bridgeID)+1] == bridgeID+"_" {
		return nil, fmt.Errorf("bridge ID %q clashes with the user ID %s", bridgeID, s.userID)
	}
	c := &Connector{server: s, bridgeID: bridgeID}
	c.bot = &Intent{server: s, conn: c, mxid: id.NewUserID(bridgeID+"bot", s.serverName)}
	s.connectors[bridgeID] = c
	return c, nil
}

func (s *Server) connector(bridgeID string) *Connector {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.connectors[bridgeID]
}

// Update is a change in the local storage, sent to subscribers. Exactly one
// field is set.
type Update struct {
	// Event is a new event stored in a room (message, state change, ...).
	Event *event.Event
	// MessageStatus is the new delivery status of a message sent by the user.
	MessageStatus *MessageStatus
	// BridgeState is the new connection state of a bridge or a login.
	BridgeState *BridgeState
}

// BridgeState is the connection state reported by a bridge for one login, or
// for the whole bridge when RemoteID is empty.
type BridgeState struct {
	BridgeID string
	status.BridgeState
}

type subscriber struct {
	ch     chan Update
	closed bool
}

// Subscribe returns a channel that receives every Update, and a function to
// unsubscribe. A subscriber that does not keep up (more than buffer pending
// updates) is dropped: its channel is closed, and it must subscribe again and
// re-read what it needs from the storage, which stays the source of truth.
func (s *Server) Subscribe(buffer int) (<-chan Update, func()) {
	sub := &subscriber{ch: make(chan Update, buffer)}
	s.lock.Lock()
	s.subscribers[sub] = struct{}{}
	s.lock.Unlock()
	return sub.ch, func() {
		s.lock.Lock()
		defer s.lock.Unlock()
		s.dropLocked(sub)
	}
}

func (s *Server) dropLocked(sub *subscriber) {
	if !sub.closed {
		sub.closed = true
		close(sub.ch)
	}
	delete(s.subscribers, sub)
}

func (s *Server) publish(update Update) {
	s.lock.Lock()
	defer s.lock.Unlock()
	for sub := range s.subscribers {
		select {
		case sub.ch <- update:
		default:
			s.log.Warn().Msg("Dropping a subscriber that does not keep up with updates")
			s.dropLocked(sub)
		}
	}
}

// SendMessage sends a message from the device's user in a room, as a Matrix
// client would: the event is stored, then handed to the bridge of the room.
// The outcome of the delivery is reported later as a MessageStatus (see
// Subscribe and Server.MessageStatus); the returned error only covers local
// failures.
func (s *Server) SendMessage(ctx context.Context, roomID id.RoomID, content *event.MessageEventContent) (id.EventID, error) {
	room, err := s.store.getRoom(ctx, roomID)
	if err != nil {
		return "", err
	}
	if membership, err := s.membership(ctx, roomID, s.userID); err != nil {
		return "", err
	} else if membership != event.MembershipJoin {
		return "", fmt.Errorf("%w: %s is not in %s", ErrForbidden, s.userID, roomID)
	}
	conn := s.connector(room.BridgeID)
	if conn == nil || conn.br == nil {
		return "", fmt.Errorf("the bridge %q of room %s is not running", room.BridgeID, roomID)
	}
	evt := &event.Event{
		ID:        newEventID(s.serverName),
		RoomID:    roomID,
		Sender:    s.userID,
		Type:      event.EventMessage,
		Timestamp: time.Now().UnixMilli(),
		Content:   event.Content{Parsed: content},
	}
	err = s.transact(ctx, func(ctx context.Context, store func(*event.Event) error) error {
		return store(evt)
	})
	if err != nil {
		return "", err
	}
	pending := &MessageStatus{RoomID: roomID, EventID: evt.ID, Status: event.MessageStatusPending, UpdatedAt: time.Now()}
	if err = s.setMessageStatus(ctx, pending); err != nil {
		return "", err
	}
	// The bridge may modify the content (reply fallbacks), so it gets a copy.
	bridged := *evt
	parsed := *content
	bridged.Content = event.Content{Parsed: &parsed}
	// The message is stored and pending first, so the application shows it
	// at once, even when the room is not a portal yet.
	if err = waitForPortal(ctx, conn.br, roomID, s.portalWait); err != nil {
		// The caller gave up (or the lookup failed): the message is stored,
		// so it must not stay pending.
		if failErr := s.failIfPending(context.WithoutCancel(ctx), roomID, evt.ID, err); failErr != nil {
			return "", errors.Join(err, failErr)
		}
		return "", err
	}
	// With a portal event buffer, this returns once the event is queued (it
	// blocks while the portal's queue is full).
	result := conn.br.QueueMatrixEvent(conn.br.Log.WithContext(ctx), &bridged)
	if !result.Queued && !result.Success {
		// Most failures were reported through SendMessageStatus already, but
		// a few paths (portal being deleted, bridge shutting down) report
		// nothing: never leave the message pending forever.
		if err = s.failIfPending(ctx, roomID, evt.ID, result.Error); err != nil {
			return "", err
		}
	}
	return evt.ID, nil
}

// DefaultPortalWait is the default of Options.PortalWait.
const DefaultPortalWait = 5 * time.Second

// waitForPortal waits until bridgev2 knows roomID as a portal. bridgev2
// creates a portal's room through the connector (CreateRoom) and only then
// records the room ID as the portal's: the room, its name and its members are
// visible here a moment before a message sent to it can be bridged, and a
// message sent in between would fail with "room is not a portal". A room that
// does not become a portal within maxWait is left to bridgev2, which reports
// that failure as the message status.
func waitForPortal(ctx context.Context, br *bridgev2.Bridge, roomID id.RoomID, maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)
	for {
		portal, err := br.GetPortalByMXID(ctx, roomID)
		if err != nil {
			return fmt.Errorf("looking up the portal of %s: %w", roomID, err)
		}
		if portal != nil || time.Now().After(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// failIfPending marks a message as failed unless the bridge already reported
// an outcome for it.
func (s *Server) failIfPending(ctx context.Context, roomID id.RoomID, eventID id.EventID, cause error) error {
	current, err := s.store.getMessageStatus(ctx, eventID)
	if err != nil {
		return err
	}
	if current != nil && current.Status != event.MessageStatusPending {
		return nil
	}
	message := "The message was not handled by the bridge."
	if cause != nil {
		message = "The message was not handled by the bridge: " + cause.Error()
	}
	return s.setMessageStatus(ctx, &MessageStatus{
		RoomID:    roomID,
		EventID:   eventID,
		Status:    event.MessageStatusFail,
		Reason:    event.MessageStatusGenericError,
		Message:   message,
		UpdatedAt: time.Now(),
	})
}

func (s *Server) setMessageStatus(ctx context.Context, st *MessageStatus) error {
	if err := s.store.setMessageStatus(ctx, st); err != nil {
		return fmt.Errorf("storing message status: %w", err)
	}
	s.publish(Update{MessageStatus: st})
	return nil
}

func (s *Server) membership(ctx context.Context, roomID id.RoomID, userID id.UserID) (event.Membership, error) {
	evt, err := s.store.getState(ctx, roomID, event.StateMember, userID.String())
	if err != nil || evt == nil {
		return event.MembershipLeave, err
	}
	return evt.Content.AsMember().Membership, nil
}

// Rooms lists every stored room, oldest first.
func (s *Server) Rooms(ctx context.Context) ([]*Room, error) {
	return s.store.listRooms(ctx)
}

// Room returns one room, or an error wrapping ErrNotFound.
func (s *Server) Room(ctx context.Context, roomID id.RoomID) (*Room, error) {
	return s.store.getRoom(ctx, roomID)
}

// Timeline returns up to limit events of a room stored after the given
// stream order (0 for the start), oldest first.
func (s *Server) Timeline(ctx context.Context, roomID id.RoomID, after int64, limit int) ([]*event.Event, error) {
	return s.store.timeline(ctx, roomID, after, limit)
}

// MessagesBefore returns up to limit message events (m.room.message) of a
// room stored before the given stream order, or the latest ones when before
// is 0, newest first. The stream order of each event is in
// Unsigned.BeeperHSOrder, to page further back.
func (s *Server) MessagesBefore(ctx context.Context, roomID id.RoomID, before int64, limit int) ([]*event.Event, error) {
	return s.store.messagesBefore(ctx, roomID, before, limit)
}

// Event returns one event of a room, or an error wrapping ErrNotFound.
func (s *Server) Event(ctx context.Context, roomID id.RoomID, eventID id.EventID) (*event.Event, error) {
	return s.store.getEvent(ctx, roomID, eventID)
}

// State returns the current state event of a room for a type and state key,
// or nil if there is none.
func (s *Server) State(ctx context.Context, roomID id.RoomID, eventType event.Type, stateKey string) (*event.Event, error) {
	return s.store.getState(ctx, roomID, eventType, stateKey)
}

// Members returns the current member events of a room, by user ID.
func (s *Server) Members(ctx context.Context, roomID id.RoomID) (map[id.UserID]*event.MemberEventContent, error) {
	events, err := s.store.getStateOfType(ctx, roomID, event.StateMember)
	if err != nil {
		return nil, err
	}
	members := make(map[id.UserID]*event.MemberEventContent, len(events))
	for _, evt := range events {
		members[id.UserID(*evt.StateKey)] = evt.Content.AsMember()
	}
	return members, nil
}

// Profile returns the global profile of a user.
func (s *Server) Profile(ctx context.Context, userID id.UserID) (*Profile, error) {
	return s.store.getProfile(ctx, userID)
}

// MessageStatus returns the delivery status of a message the user sent, or
// nil if the message is unknown.
func (s *Server) MessageStatus(ctx context.Context, eventID id.EventID) (*MessageStatus, error) {
	return s.store.getMessageStatus(ctx, eventID)
}

// BridgeState returns the last state a bridge reported for a login (or for
// the whole bridge when loginID is empty), or nil if it reported none.
func (s *Server) BridgeState(ctx context.Context, bridgeID, loginID string) (*BridgeState, error) {
	data, err := s.store.getBridgeState(ctx, bridgeID, loginID)
	if err != nil || data == nil {
		return nil, err
	}
	state := &BridgeState{BridgeID: bridgeID}
	if err = json.Unmarshal(data, &state.BridgeState); err != nil {
		return nil, err
	}
	return state, nil
}
