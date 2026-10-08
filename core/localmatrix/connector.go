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

// ErrBatchSendUnsupported is returned by BatchSend: the connector does not
// advertise batch sending, so bridgev2 sends backfilled messages one by one.
var ErrBatchSendUnsupported = errors.New("batch sending is not supported by the local Matrix connector")

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
// no other client to accept invites) and that batch sending is unavailable.
func (c *Connector) GetCapabilities() *bridgev2.MatrixCapabilities {
	return &bridgev2.MatrixCapabilities{AutoJoinInvites: true}
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
func (c *Connector) SendBridgeStatus(ctx context.Context, state *status.BridgeState) error {
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

// BatchSend is not supported (see GetCapabilities).
func (c *Connector) BatchSend(context.Context, id.RoomID, *mautrix.ReqBeeperBatchSend, []*bridgev2.MatrixSendExtra) (*mautrix.RespBeeperBatchSend, error) {
	return nil, ErrBatchSendUnsupported
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
