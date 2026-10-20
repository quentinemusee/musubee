// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package echo

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/util/ptr"
	"go.mau.fi/util/random"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"
)

// ErrUnreachable is the reason why sending to ContactUnreachable fails.
var ErrUnreachable = errors.New("the contact is unreachable")

// client is the bridgev2.NetworkAPI of one login.
type client struct {
	connector *Connector
	login     *bridgev2.UserLogin

	lock      sync.Mutex
	connected bool
	// fresh is set for a login just made: an offline or expired login
	// (OfflinePrefix, ExpiredPrefix) connects only then.
	fresh bool
	// ctx is cancelled on disconnection, which drops the pending echoes.
	ctx    context.Context
	cancel context.CancelFunc
	echoes sync.WaitGroup
}

var (
	_ bridgev2.NetworkAPI                    = (*client)(nil)
	_ bridgev2.ReadReceiptHandlingNetworkAPI = (*client)(nil)
)

func newClient(connector *Connector, login *bridgev2.UserLogin) *client {
	return &client{connector: connector, login: login}
}

func (c *client) selfID() networkid.UserID {
	return networkid.UserID(selfIDPrefix + string(c.login.ID))
}

func (c *client) portalKey(portal networkid.PortalID) networkid.PortalKey {
	return networkid.PortalKey{ID: portal, Receiver: c.login.ID}
}

// Errors of offline and expired logins (OfflinePrefix, ExpiredPrefix).
const (
	errOffline status.BridgeStateErrorCode = "echo-offline"
	errExpired status.BridgeStateErrorCode = "echo-expired"
)

// Connect marks the login as connected and announces every conversation,
// which creates the rooms on first connection.
func (c *client) Connect(ctx context.Context) {
	c.lock.Lock()
	// A connection started in the background may run once the bridge is
	// stopping, after Disconnect: it must not reconnect.
	if c.connected || c.connector.br.IsStopping() {
		c.lock.Unlock()
		return
	}
	if strings.HasPrefix(c.login.RemoteName, OfflinePrefix) && !c.fresh {
		c.lock.Unlock()
		c.login.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateTransientDisconnect,
			Error:      errOffline,
			Message:    "No network: the echo network simulates a device without connection.",
		})
		return
	}
	if strings.HasPrefix(c.login.RemoteName, ExpiredPrefix) && !c.fresh {
		c.lock.Unlock()
		c.login.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateBadCredentials,
			Error:      errExpired,
			Message:    "Session ended: the echo network simulates a session that the network ended.",
		})
		return
	}
	c.fresh = false
	c.connected = true
	c.ctx, c.cancel = context.WithCancel(c.connector.br.BackgroundCtx)
	c.lock.Unlock()

	c.login.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
	c.updateSelfGhost(ctx)
	contacts := contactsOf(c.login)
	for _, contact := range contacts {
		c.login.QueueRemoteEvent(&simplevent.ChatResync{
			EventMeta: simplevent.EventMeta{
				Type:         bridgev2.RemoteEventChatResync,
				PortalKey:    c.portalKey(networkid.PortalID(contact.ID)),
				CreatePortal: true,
			},
			ChatInfo: c.chatInfo(contact),
		})
	}
	for _, group := range Groups {
		c.login.QueueRemoteEvent(&simplevent.ChatResync{
			EventMeta: simplevent.EventMeta{
				Type:         bridgev2.RemoteEventChatResync,
				PortalKey:    c.portalKey(group.ID),
				CreatePortal: true,
			},
			ChatInfo: c.groupInfo(group),
		})
	}
	c.greetFromCrowd(contacts)
}

// greetFromCrowd sends the one message of each crowd contact, a minute
// apart, the last contact most recently: their order by activity is the
// reverse of their names'. The messages have fixed IDs, so that bridgev2
// drops them as duplicates on the next connections.
func (c *client) greetFromCrowd(contacts []Contact) {
	now := time.Now()
	for _, contact := range contacts {
		number, ok := strings.CutPrefix(string(contact.ID), crowdIDPrefix)
		if !ok {
			continue
		}
		n, _ := strconv.Atoi(number)
		c.login.QueueRemoteEvent(&simplevent.Message[*event.MessageEventContent]{
			EventMeta: simplevent.EventMeta{
				Type:      bridgev2.RemoteEventMessage,
				PortalKey: c.portalKey(networkid.PortalID(contact.ID)),
				Sender:    bridgev2.EventSender{Sender: contact.ID},
				Timestamp: now.Add(-time.Duration(CrowdSize-n) * time.Minute),
			},
			ID:                 networkid.MessageID("hello-" + number),
			Data:               &event.MessageEventContent{MsgType: event.MsgText, Body: "Hello from " + contact.Name},
			ConvertMessageFunc: convertEcho,
		})
	}
}

// updateSelfGhost fills in the profile of the user's own ghost before the
// conversations are announced. Every conversation lists that ghost, bridgev2
// creates the conversations in parallel, and in mautrix-go v0.31.0
// Ghost.UpdateInfoIfNecessary reads and writes the ghost's fields without a
// lock: the first update of a ghost shared by several conversations is a data
// race (docs/ADR/0009). Once the profile is set, the portals only read it.
func (c *client) updateSelfGhost(ctx context.Context) {
	ghost, err := c.connector.br.GetGhostByID(ctx, c.selfID())
	if err != nil {
		c.login.Log.Err(err).Msg("Failed to get the user's own ghost")
		return
	}
	ghost.UpdateInfoIfNecessary(ctx, c.login, bridgev2.RemoteEventUnknown)
}

// Disconnect drops the pending delayed echoes, like a real network would
// lose the replies that arrive while the app is offline.
func (c *client) Disconnect() {
	c.lock.Lock()
	c.connected = false
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.lock.Unlock()
	c.echoes.Wait()
}

func (c *client) IsLoggedIn() bool {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.connected
}

// LogoutRemote does nothing: the fake network keeps no session.
func (c *client) LogoutRemote(context.Context) {}

func (c *client) IsThisUser(_ context.Context, userID networkid.UserID) bool {
	return userID == c.selfID()
}

func (c *client) chatInfo(contact Contact) *bridgev2.ChatInfo {
	return &bridgev2.ChatInfo{
		Name: ptr.Ptr(contact.Name),
		Type: ptr.Ptr(database.RoomTypeDM),
		Members: &bridgev2.ChatMemberList{
			IsFull:      true,
			OtherUserID: contact.ID,
			MemberMap: bridgev2.ChatMemberMap{
				c.selfID(): {
					EventSender: bridgev2.EventSender{IsFromMe: true, Sender: c.selfID()},
					Membership:  event.MembershipJoin,
				},
				contact.ID: {
					EventSender: bridgev2.EventSender{Sender: contact.ID},
					Membership:  event.MembershipJoin,
					UserInfo:    userInfo(contact),
				},
			},
		},
	}
}

func (c *client) groupInfo(group Group) *bridgev2.ChatInfo {
	members := bridgev2.ChatMemberMap{
		c.selfID(): {
			EventSender: bridgev2.EventSender{IsFromMe: true, Sender: c.selfID()},
			Membership:  event.MembershipJoin,
		},
	}
	for _, id := range group.Members {
		contact, _ := findContact(id)
		members[id] = bridgev2.ChatMember{
			EventSender: bridgev2.EventSender{Sender: id},
			Membership:  event.MembershipJoin,
			UserInfo:    userInfo(contact),
		}
	}
	return &bridgev2.ChatInfo{
		Name:    ptr.Ptr(group.Name),
		Type:    ptr.Ptr(database.RoomTypeDefault),
		Members: &bridgev2.ChatMemberList{IsFull: true, MemberMap: members},
	}
}

func userInfo(contact Contact) *bridgev2.UserInfo {
	return &bridgev2.UserInfo{
		Name:        ptr.Ptr(contact.Name),
		Identifiers: []string{"echo:" + string(contact.ID)},
	}
}

func (c *client) GetChatInfo(_ context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	if group, ok := findGroup(portal.ID); ok {
		return c.groupInfo(group), nil
	}
	contact, ok := findContact(networkid.UserID(portal.ID))
	if !ok {
		return nil, fmt.Errorf("unknown conversation %q", portal.ID)
	}
	return c.chatInfo(contact), nil
}

func (c *client) GetUserInfo(_ context.Context, ghost *bridgev2.Ghost) (*bridgev2.UserInfo, error) {
	if ghost.ID == c.selfID() {
		return &bridgev2.UserInfo{Name: ptr.Ptr(c.login.RemoteName)}, nil
	}
	contact, ok := findContact(ghost.ID)
	if !ok {
		return nil, fmt.Errorf("unknown user %q", ghost.ID)
	}
	return userInfo(contact), nil
}

// GetCapabilities returns what the conversations support: plain and
// formatted text only.
func (c *client) GetCapabilities(context.Context, *bridgev2.Portal) *event.RoomFeatures {
	return &event.RoomFeatures{
		ID:            "com.musubee.echo.capabilities.v1",
		MaxTextLength: 4096,
	}
}

// HandleMatrixMessage "sends" a message the user wrote, then echoes it back
// depending on the contact.
func (c *client) HandleMatrixMessage(_ context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	contact, ok := findContact(networkid.UserID(msg.Portal.ID))
	if group, isGroup := findGroup(msg.Portal.ID); isGroup {
		// In a group, its first member answers.
		contact, ok = findContact(group.Members[0])
	}
	if !ok {
		return nil, bridgev2.WrapErrorInStatus(fmt.Errorf("unknown conversation %q", msg.Portal.ID)).
			WithStatus(event.MessageStatusFail).
			WithErrorAsMessage().
			WithIsCertain(true).
			WithSendNotice(false)
	}
	if contact.ID == ContactUnreachable {
		return nil, bridgev2.WrapErrorInStatus(ErrUnreachable).
			WithStatus(event.MessageStatusFail).
			WithErrorReason(event.MessageStatusNetworkError).
			WithMessage("The message was not delivered: the contact is unreachable.").
			WithIsCertain(true).
			WithSendNotice(false)
	}
	var delay time.Duration
	if contact.ID == ContactDelayed {
		delay = c.connector.Config.EchoDelay
	}
	sent := newMessageID()
	answer := func() []bridgev2.RemoteEvent { return c.answer(msg.Portal.ID, contact, sent, msg.Content) }
	if err := c.schedule(delay, answer); err != nil {
		return nil, err
	}
	return &bridgev2.MatrixMessageResponse{
		DB: &database.Message{
			ID:        sent,
			SenderID:  c.selfID(),
			Timestamp: time.Now(),
		},
	}, nil
}

// The commands that make a contact act on messages (see the package's
// documentation).
const (
	CommandReact  = "/react"
	CommandEdit   = "/edit"
	CommandDelete = "/delete"
	CommandReply  = "/reply"
)

// EditedSuffix ends the text of an echo that CommandEdit edited.
const EditedSuffix = " (edited)"

// command splits a message's text into a command and the rest, or returns
// no command.
func command(body string) (string, string) {
	for _, cmd := range []string{CommandReact, CommandEdit, CommandDelete, CommandReply} {
		if rest, ok := strings.CutPrefix(body, cmd); ok && (rest == "" || rest[0] == ' ') {
			return cmd, strings.TrimSpace(rest)
		}
	}
	return "", body
}

// answer returns what the contact does after the user sent a message: an
// echo, or what its command asks for.
func (c *client) answer(portal networkid.PortalID, contact Contact, sent networkid.MessageID, received *event.MessageEventContent) []bridgev2.RemoteEvent {
	meta := func(eventType bridgev2.RemoteEventType) simplevent.EventMeta {
		return simplevent.EventMeta{
			Type:      eventType,
			PortalKey: c.portalKey(portal),
			Sender:    bridgev2.EventSender{Sender: contact.ID},
			Timestamp: time.Now(),
		}
	}
	cmd, text := command(received.Body)
	if cmd == CommandReact {
		if text == "" {
			text = "👍"
		}
		return []bridgev2.RemoteEvent{&simplevent.Reaction{
			EventMeta:     meta(bridgev2.RemoteEventReaction),
			TargetMessage: sent,
			EmojiID:       networkid.EmojiID(text),
			Emoji:         text,
		}}
	}
	content := *received
	content.RelatesTo = nil
	if cmd != "" {
		content.Body, content.Format, content.FormattedBody = text, "", ""
	}
	echo := &simplevent.Message[*event.MessageEventContent]{
		EventMeta:          meta(bridgev2.RemoteEventMessage),
		ID:                 newMessageID(),
		Data:               &content,
		ConvertMessageFunc: convertEcho,
	}
	switch cmd {
	case CommandReply:
		echo.ConvertMessageFunc = func(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, content *event.MessageEventContent) (*bridgev2.ConvertedMessage, error) {
			converted, err := convertEcho(ctx, portal, intent, content)
			if err == nil {
				converted.ReplyTo = &networkid.MessageOptionalPartID{MessageID: sent}
			}
			return converted, err
		}
	case CommandEdit:
		edited := content
		edited.Body += EditedSuffix
		return []bridgev2.RemoteEvent{echo, &simplevent.Message[*event.MessageEventContent]{
			EventMeta:       meta(bridgev2.RemoteEventEdit),
			TargetMessage:   echo.ID,
			Data:            &edited,
			ConvertEditFunc: convertEdit,
		}}
	case CommandDelete:
		return []bridgev2.RemoteEvent{echo, &simplevent.MessageRemove{
			EventMeta:     meta(bridgev2.RemoteEventMessageRemove),
			TargetMessage: echo.ID,
		}}
	}
	return []bridgev2.RemoteEvent{echo}
}

func newMessageID() networkid.MessageID {
	return networkid.MessageID(random.String(16))
}

// errDisconnected is returned when a message is sent while the client is
// disconnected. Like every error a connector returns, it carries a reason
// and a message: a bare error reaches the app as a retriable failure with
// no explanation.
var errDisconnected = bridgev2.WrapErrorInStatus(errors.New("the echo network client is disconnected")).
	WithErrorReason(event.MessageStatusNetworkError).
	WithErrorAsMessage().
	WithSendNotice(false)

// schedule queues remote events, in order, after a delay; they are made
// then, so that they are dated then.
func (c *client) schedule(delay time.Duration, events func() []bridgev2.RemoteEvent) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	if !c.connected {
		return errDisconnected
	}
	ctx := c.ctx
	c.echoes.Add(1)
	go func() {
		defer c.echoes.Done()
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				return
			}
		}
		for _, evt := range events() {
			c.login.QueueRemoteEvent(evt)
		}
	}()
	return nil
}

func convertEcho(_ context.Context, _ *bridgev2.Portal, _ bridgev2.MatrixAPI, content *event.MessageEventContent) (*bridgev2.ConvertedMessage, error) {
	return &bridgev2.ConvertedMessage{
		Parts: []*bridgev2.ConvertedMessagePart{{
			Type:    event.EventMessage,
			Content: content,
		}},
	}, nil
}

func convertEdit(_ context.Context, _ *bridgev2.Portal, _ bridgev2.MatrixAPI, existing []*database.Message, content *event.MessageEventContent) (*bridgev2.ConvertedEdit, error) {
	return &bridgev2.ConvertedEdit{
		ModifiedParts: []*bridgev2.ConvertedEditPart{{
			Part:    existing[0],
			Type:    event.EventMessage,
			Content: content,
		}},
	}, nil
}

// HandleMatrixReadReceipt accepts the user's read receipts: the echo network
// has nobody to tell, and bridgev2 records how far the login read
// (database.UserPortal.LastRead), which the tests check.
func (c *client) HandleMatrixReadReceipt(context.Context, *bridgev2.MatrixReadReceipt) error {
	return nil
}
