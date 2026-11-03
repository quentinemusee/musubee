// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package localmatrix

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Connector is the bridgev2.MatrixConnector of one bridge. It stores
// everything the bridge sends to "Matrix" in the Server's storage.
type Connector struct {
	server   *Server
	bridgeID string
	br       *bridgev2.Bridge
	bot      *Intent
}

var (
	_ bridgev2.MatrixConnector                       = (*Connector)(nil)
	_ bridgev2.MatrixConnectorWithArbitraryRoomState = (*Connector)(nil)
)

// Init is called by bridgev2.NewBridge.
func (c *Connector) Init(br *bridgev2.Bridge) {
	c.br = br
}

// Start creates or upgrades the local tables.
func (c *Connector) Start(ctx context.Context) error {
	return c.server.Upgrade(ctx)
}

// PreStop does nothing: there is no connection to close.
func (c *Connector) PreStop() {}

// Stop does nothing: there is no connection to close.
func (c *Connector) Stop() {}

// GetCapabilities tells bridgev2 that invited users join immediately (there is
// no other client to accept invites) and that history comes in batches
// (BatchSend), which can go before the stored messages.
func (c *Connector) GetCapabilities() *bridgev2.MatrixCapabilities {
	return &bridgev2.MatrixCapabilities{AutoJoinInvites: true, BatchSending: true}
}

func (c *Connector) ghostPrefix() string {
	return "@" + c.bridgeID + "_"
}

// ParseGhostMXID returns the remote user ID of one of this bridge's ghosts.
func (c *Connector) ParseGhostMXID(userID id.UserID) (networkid.UserID, bool) {
	localpart, server, err := userID.Parse()
	if err != nil || server != c.server.serverName || !strings.HasPrefix(userID.String(), c.ghostPrefix()) {
		return "", false
	}
	decoded, err := id.DecodeUserLocalpart(strings.TrimPrefix(localpart, c.bridgeID+"_"))
	if err != nil {
		return "", false
	}
	return networkid.UserID(decoded), true
}

// FormatGhostMXID returns the Matrix ID of the ghost of a remote user.
func (c *Connector) FormatGhostMXID(userID networkid.UserID) id.UserID {
	return id.NewUserID(c.bridgeID+"_"+id.EncodeUserLocalpart(string(userID)), c.server.serverName)
}

// GhostIntent returns the API used to act as a remote user.
func (c *Connector) GhostIntent(userID networkid.UserID) bridgev2.MatrixAPI {
	return &Intent{server: c.server, conn: c, mxid: c.FormatGhostMXID(userID)}
}

// NewUserIntent returns the API used to act as the device's user ("double
// puppeting"), so that messages the user sent from another app on the network
// appear as their own. Only the device's user can be puppeted, and no access
// token is needed.
func (c *Connector) NewUserIntent(_ context.Context, userID id.UserID, accessToken string) (bridgev2.MatrixAPI, string, error) {
	if userID != c.server.userID {
		return nil, "", errors.New("only the device's user can be puppeted")
	}
	return &Intent{server: c.server, conn: c, mxid: userID, doublePuppet: true}, accessToken, nil
}

// BotIntent returns the API of the bridge bot, which creates the rooms.
func (c *Connector) BotIntent() bridgev2.MatrixAPI {
	return c.bot
}

// SendBridgeStatus stores the connection state of the bridge or of a login.
// After Server.Stop, it drops the state.
func (c *Connector) SendBridgeStatus(ctx context.Context, state *status.BridgeState) error {
	c.server.stopLock.RLock()
	defer c.server.stopLock.RUnlock()
	if c.server.stopped {
		return nil
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err = c.server.store.setBridgeState(ctx, c.bridgeID, string(state.RemoteID), data, time.Now()); err != nil {
		return err
	}
	c.server.publish(Update{BridgeState: &BridgeState{BridgeID: c.bridgeID, BridgeState: *state}})
	return nil
}

// SendMessageStatus stores the delivery status of a message the user sent.
func (c *Connector) SendMessageStatus(ctx context.Context, ms *bridgev2.MessageStatus, info *bridgev2.MessageStatusEventInfo) {
	if info.SourceEventID == "" || info.Sender != c.server.userID {
		return
	}
	content := ms.ToMSSEvent(info)
	err := c.server.setMessageStatus(ctx, &MessageStatus{
		RoomID:     info.RoomID,
		EventID:    info.SourceEventID,
		Status:     content.Status,
		Reason:     content.Reason,
		Message:    content.Message,
		NewEventID: info.NewEventID,
		UpdatedAt:  time.Now(),
	})
	if err != nil {
		c.br.Log.Err(err).Stringer("event_id", info.SourceEventID).Msg("Failed to store message status")
	}
}

// GenerateContentURI is only used for direct media, which is not enabled.
func (c *Connector) GenerateContentURI(context.Context, networkid.MediaID) (id.ContentURIString, error) {
	return "", bridgev2.ErrDirectMediaNotEnabled
}

// ParseContentURI is only used for direct media, which is not enabled.
func (c *Connector) ParseContentURI(context.Context, id.ContentURIString) (networkid.MediaID, error) {
	return nil, bridgev2.ErrDirectMediaNotEnabled
}

// GetPowerLevels returns the power levels of a room.
func (c *Connector) GetPowerLevels(ctx context.Context, roomID id.RoomID) (*event.PowerLevelsEventContent, error) {
	evt, err := c.server.store.getState(ctx, roomID, event.StatePowerLevels, "")
	if err != nil {
		return nil, err
	} else if evt == nil {
		return &event.PowerLevelsEventContent{}, nil
	}
	return evt.Content.AsPowerLevels(), nil
}

// GetMembers returns the member events of a room.
func (c *Connector) GetMembers(ctx context.Context, roomID id.RoomID) (map[id.UserID]*event.MemberEventContent, error) {
	return c.server.Members(ctx, roomID)
}

// GetMemberInfo returns the member event of a user, or nil.
func (c *Connector) GetMemberInfo(ctx context.Context, roomID id.RoomID, userID id.UserID) (*event.MemberEventContent, error) {
	evt, err := c.server.store.getState(ctx, roomID, event.StateMember, userID.String())
	if err != nil || evt == nil {
		return nil, err
	}
	return evt.Content.AsMember(), nil
}

// GetStateEvent returns a state event of a room, or nil.
func (c *Connector) GetStateEvent(ctx context.Context, roomID id.RoomID, eventType event.Type, stateKey string) (*event.Event, error) {
	return c.server.store.getState(ctx, roomID, eventType, stateKey)
}

// BatchSend stores a batch of history (docs/ADR/0041), as Beeper's batch
// send API does on a homeserver: after the stored events (forward), or
// before every stored message (backward), as bridgev2 asks; a backward
// batch goes forward in a room without messages when asked to
// (ForwardIfNoMessages). The senders join first. An event already stored
// (by its deterministic ID) is skipped.
//
// A forward batch is published like any event, so that the core tells the
// interface. A backward batch is not: it is history that the interface
// asked for (messages.fetchOlder) and reads back; it is never unread
// either, since the read position moves past it when the room has none.
func (c *Connector) BatchSend(ctx context.Context, roomID id.RoomID, req *mautrix.ReqBeeperBatchSend, _ []*bridgev2.MatrixSendExtra) (*mautrix.RespBeeperBatchSend, error) {
	s := c.server
	resp := &mautrix.RespBeeperBatchSend{EventIDs: make([]id.EventID, len(req.Events))}
	var readChanged bool
	err := s.transact(ctx, func(ctx context.Context, store func(*event.Event) error) error {
		if _, err := s.store.getRoom(ctx, roomID); err != nil {
			return err
		}
		forward := req.Forward
		if !forward && req.ForwardIfNoMessages {
			has, err := s.store.hasMessages(ctx, roomID)
			if err != nil {
				return err
			}
			forward = !has
		}
		joined := make(map[id.UserID]bool)
		for _, evt := range req.Events {
			if joined[evt.Sender] {
				continue
			}
			joined[evt.Sender] = true
			sender := &Intent{server: s, conn: c, mxid: evt.Sender, doublePuppet: evt.Sender == s.userID}
			if err := sender.ensureJoined(ctx, roomID, store); err != nil {
				return err
			}
		}
		var next int64
		if !forward {
			var err error
			if next, err = s.store.backwardOrders(ctx, len(req.Events)); err != nil {
				return err
			}
		}
		var last *event.Event
		for i, original := range req.Events {
			evt := *original
			evt.RoomID = roomID
			if evt.ID == "" {
				evt.ID = newEventID(s.serverName)
			}
			if evt.Type.Class == event.UnknownEventType {
				evt.Type.Class = event.MessageEventType
			}
			if evt.Timestamp == 0 {
				evt.Timestamp = time.Now().UnixMilli()
			}
			resp.EventIDs[i] = evt.ID
			exists, err := s.store.eventExists(ctx, evt.ID)
			if err != nil {
				return err
			} else if exists {
				continue
			}
			if forward {
				err = store(&evt)
			} else {
				err = s.store.insertEventAt(ctx, &evt, next)
				next++
			}
			if err != nil {
				return err
			}
			last = &evt
		}
		if last == nil {
			return nil
		}
		// Read as bridgev2 asks, and older history is never news, even when
		// it goes forward in an empty room: without a read position, the
		// room gets one past it (an existing one is already above, and only
		// moves forward).
		if req.MarkReadBy == s.userID || !req.Forward {
			var err error
			readChanged, err = s.store.advanceReadMarker(ctx, roomID, last.Unsigned.BeeperHSOrder, time.Now())
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if readChanged {
		s.publish(Update{ReadState: roomID})
	}
	return resp, nil
}

// GenerateDeterministicRoomID returns the room ID of a portal, which is the
// same every time the portal is created again.
func (c *Connector) GenerateDeterministicRoomID(key networkid.PortalKey) id.RoomID {
	return id.RoomID("!" + hashID("room", c.bridgeID, string(key.ID), string(key.Receiver)) + ":" + c.server.serverName)
}

// GenerateDeterministicEventID returns a stable event ID for a message part.
func (c *Connector) GenerateDeterministicEventID(roomID id.RoomID, key networkid.PortalKey, messageID networkid.MessageID, partID networkid.PartID) id.EventID {
	return id.EventID("$" + hashID("event", c.bridgeID, roomID.String(), string(key.ID), string(key.Receiver), string(messageID), string(partID)) + ":" + c.server.serverName)
}

// GenerateReactionEventID returns a stable event ID for a reaction.
func (c *Connector) GenerateReactionEventID(roomID id.RoomID, target *database.Message, sender networkid.UserID, emojiID networkid.EmojiID) id.EventID {
	return id.EventID("$" + hashID("reaction", c.bridgeID, roomID.String(), string(target.ID), string(target.PartID), string(sender), string(emojiID)) + ":" + c.server.serverName)
}

// ServerName is the server part of the local identifiers.
func (c *Connector) ServerName() string {
	return c.server.serverName
}
