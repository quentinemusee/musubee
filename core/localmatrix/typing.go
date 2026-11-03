// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package localmatrix

import (
	"context"
	"fmt"
	"slices"
	"time"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Typing notifications (docs/ADR/0048), in memory only: who types is news
// for a few seconds, never history.

// maxTyping bounds how long someone is shown typing without a renewal.
const maxTyping = 30 * time.Second

// setTyping records that a user types in a room until a time, or stopped
// (a zero time), and tells the subscribers. When the time passes, they are
// told again.
func (s *Server) setTyping(roomID id.RoomID, userID id.UserID, until time.Time) {
	s.typingLock.Lock()
	if s.typing == nil {
		s.typing = map[id.RoomID]map[id.UserID]time.Time{}
	}
	room := s.typing[roomID]
	if room == nil {
		room = map[id.UserID]time.Time{}
		s.typing[roomID] = room
	}
	_, was := room[userID]
	if until.IsZero() {
		delete(room, userID)
	} else {
		room[userID] = until
	}
	s.typingLock.Unlock()
	if was || !until.IsZero() {
		s.publish(Update{Typing: roomID})
	}
	if !until.IsZero() {
		time.AfterFunc(time.Until(until)+10*time.Millisecond, func() { s.expireTyping(roomID, userID) })
	}
}

// expireTyping forgets a user's typing once its time passed, unless renewed.
func (s *Server) expireTyping(roomID id.RoomID, userID id.UserID) {
	s.typingLock.Lock()
	until, ok := s.typing[roomID][userID]
	expired := ok && !time.Now().Before(until)
	if expired {
		delete(s.typing[roomID], userID)
	}
	s.typingLock.Unlock()
	if expired {
		s.publish(Update{Typing: roomID})
	}
}

// Typing returns who types in a room now, sorted.
func (s *Server) Typing(roomID id.RoomID) []id.UserID {
	s.typingLock.Lock()
	defer s.typingLock.Unlock()
	var users []id.UserID
	for userID, until := range s.typing[roomID] {
		if time.Now().Before(until) {
			users = append(users, userID)
		}
	}
	slices.Sort(users)
	return users
}

// SendTyping tells the room's bridge that the device's user types, or
// stopped. bridgev2 hands a change only (Portal.handleMatrixTyping): a
// renewal while typing is sent as a stop, then a start.
func (s *Server) SendTyping(ctx context.Context, roomID id.RoomID, typing bool) error {
	room, err := s.store.getRoom(ctx, roomID)
	if err != nil {
		return err
	}
	conn := s.connector(room.BridgeID)
	if conn == nil || conn.br == nil {
		return fmt.Errorf("the bridge %q of room %s is not running", room.BridgeID, roomID)
	}
	send := func(users []id.UserID) {
		conn.br.QueueMatrixEvent(conn.br.Log.WithContext(ctx), &event.Event{
			Type:    event.EphemeralEventTyping,
			RoomID:  roomID,
			Content: event.Content{Parsed: &event.TypingEventContent{UserIDs: users}},
		})
	}
	if !typing {
		send([]id.UserID{})
		return nil
	}
	send([]id.UserID{})
	send([]id.UserID{s.userID})
	return nil
}
