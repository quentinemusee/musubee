// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"errors"
	"runtime"
	"slices"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/localmatrix"
)

// This file translates the bridges and the local Matrix storage into the
// API's domain objects. No Matrix type crosses it towards the API.

const (
	defaultMessageLimit = 50
	maxMessageLimit     = 200
)

func (c *Core) hello(context.Context, api.Empty) (api.HelloResult, error) {
	return api.HelloResult{APIVersion: api.APIVersion, CoreVersion: Version}, nil
}

func (c *Core) networksList(context.Context, api.Empty) (api.NetworksListResult, error) {
	result := api.NetworksListResult{Networks: make([]api.Network, 0, len(c.networks))}
	for _, network := range c.networks {
		br := c.host.Bridge(network)
		flows := br.Network.GetLoginFlows()
		n := api.Network{NetworkID: string(network), Name: br.Network.GetName().DisplayName, LoginFlows: make([]api.LoginFlow, 0, len(flows))}
		for _, flow := range flows {
			n.LoginFlows = append(n.LoginFlows, api.LoginFlow{FlowID: flow.ID, Name: flow.Name, Description: flow.Description})
		}
		result.Networks = append(result.Networks, n)
	}
	return result, nil
}

func (c *Core) accountsList(ctx context.Context, _ api.Empty) (api.AccountsListResult, error) {
	result := api.AccountsListResult{Accounts: []api.Account{}}
	for _, network := range c.networks {
		user, err := c.host.User(ctx, network)
		if err != nil {
			return result, err
		}
		for _, login := range user.GetUserLogins() {
			account, err := c.account(ctx, network, login.ID, login.RemoteName)
			if err != nil {
				return result, err
			}
			result.Accounts = append(result.Accounts, account)
		}
	}
	return result, nil
}

func (c *Core) account(ctx context.Context, network networkid.BridgeID, login networkid.UserLoginID, name string) (api.Account, error) {
	account := api.Account{AccountID: accountID(network, login), NetworkID: string(network), Name: name, State: api.AccountStateConnecting}
	state, err := c.host.Matrix.BridgeState(ctx, string(network), string(login))
	if err != nil || state == nil {
		return account, err
	}
	account.State = accountState(state.StateEvent)
	if account.State != api.AccountStateConnected {
		account.Error = state.Message
	}
	return account, nil
}

func accountState(s status.BridgeStateEvent) api.AccountState {
	switch s {
	case status.StateStarting, status.StateConnecting, status.StateBackfilling, status.StateRunning, status.StateUnconfigured:
		return api.AccountStateConnecting
	case status.StateConnected:
		return api.AccountStateConnected
	case status.StateTransientDisconnect, status.StateBridgeUnreachable:
		return api.AccountStateTransientDisconnect
	case status.StateBadCredentials:
		return api.AccountStateBadCredentials
	case status.StateLoggedOut:
		return api.AccountStateLoggedOut
	default:
		return api.AccountStateUnknownError
	}
}

func (c *Core) conversationsList(ctx context.Context, p api.ConversationsListParams) (api.ConversationsListResult, error) {
	result := api.ConversationsListResult{Conversations: []api.Conversation{}}
	if p.AccountID != "" {
		if _, _, err := parseAccountID(p.AccountID); err != nil {
			return result, err
		}
	}
	rooms, err := c.host.Matrix.Rooms(ctx)
	if err != nil {
		return result, err
	}
	for _, room := range rooms {
		conv, err := c.conversation(ctx, room)
		if err != nil {
			return result, err
		}
		if conv != nil && (p.AccountID == "" || conv.AccountID == p.AccountID) {
			result.Conversations = append(result.Conversations, *conv)
		}
	}
	return result, nil
}

// conversation returns the conversation of a room, or nil while the room is
// not complete yet: bridgev2 names the room and records it as a portal a
// moment after creating it.
func (c *Core) conversation(ctx context.Context, room *localmatrix.Room) (*api.Conversation, error) {
	br := c.host.Bridge(networkid.BridgeID(room.BridgeID))
	if br == nil {
		return nil, nil
	}
	name, err := c.host.Matrix.State(ctx, room.ID, event.StateRoomName, "")
	if err != nil || name == nil {
		return nil, err
	}
	portal, err := br.GetPortalByMXID(ctx, room.ID)
	if err != nil || portal == nil {
		return nil, err
	}
	receiver := portal.Receiver
	if receiver == "" {
		// A portal shared by the user's logins: any of them reaches it.
		logins, err := br.GetUserLoginsInPortal(ctx, portal.PortalKey)
		if err != nil || len(logins) == 0 {
			return nil, err
		}
		receiver = logins[0].ID
	}
	kind := api.ConversationKindGroup
	if portal.RoomType == database.RoomTypeDM {
		kind = api.ConversationKindDirect
	}
	return &api.Conversation{
		ConversationID: conversationID(room.ID),
		AccountID:      accountID(br.ID, receiver),
		NetworkID:      string(br.ID),
		Name:           name.Content.AsRoomName().Name,
		Kind:           kind,
	}, nil
}

// room returns the room of a conversation ID, or a not_found error.
func (c *Core) room(ctx context.Context, conversation string) (id.RoomID, error) {
	roomID, err := parseConversationID(conversation)
	if err != nil {
		return "", err
	}
	if _, err = c.host.Matrix.Room(ctx, roomID); errors.Is(err, localmatrix.ErrNotFound) {
		return "", newError(api.ErrorCodeNotFound, "no conversation %q", conversation)
	}
	return roomID, err
}

func (c *Core) messagesList(ctx context.Context, p api.MessagesListParams) (api.MessagesListResult, error) {
	result := api.MessagesListResult{Messages: []api.Message{}}
	limit := int(p.Limit)
	if p.Limit == 0 {
		limit = defaultMessageLimit
	} else if p.Limit < 1 || p.Limit > maxMessageLimit {
		return result, newError(api.ErrorCodeInvalidParams, "limit must be between 1 and %d", maxMessageLimit)
	}
	roomID, err := c.room(ctx, p.ConversationID)
	if err != nil {
		return result, err
	}
	var before int64
	if p.Before != "" {
		if before, err = parseCursor(p.Before); err != nil {
			return result, err
		}
	}
	// One more than asked tells whether older messages remain.
	events, err := c.host.Matrix.MessagesBefore(ctx, roomID, before, limit+1)
	if err != nil {
		return result, err
	}
	if len(events) > limit {
		events = events[:limit]
		result.Before = cursor(events[limit-1].Unsigned.BeeperHSOrder)
	}
	slices.Reverse(events)
	for _, evt := range events {
		msg, err := c.message(ctx, evt)
		if err != nil {
			return result, err
		}
		result.Messages = append(result.Messages, msg)
	}
	return result, nil
}

func (c *Core) messagesSend(ctx context.Context, p api.MessagesSendParams) (api.MessagesSendResult, error) {
	if p.Text == "" {
		return api.MessagesSendResult{}, newError(api.ErrorCodeInvalidParams, "text is empty")
	}
	roomID, err := c.room(ctx, p.ConversationID)
	if err != nil {
		return api.MessagesSendResult{}, err
	}
	eventID, err := c.host.Matrix.SendMessage(ctx, roomID, &event.MessageEventContent{MsgType: event.MsgText, Body: p.Text})
	if err != nil {
		return api.MessagesSendResult{}, err
	}
	evt, err := c.host.Matrix.Event(ctx, roomID, eventID)
	if err != nil {
		return api.MessagesSendResult{}, err
	}
	msg, err := c.message(ctx, evt)
	return api.MessagesSendResult{Message: msg}, err
}

var messageKinds = map[event.MessageType]api.MessageKind{
	event.MsgText:   api.MessageKindText,
	event.MsgNotice: api.MessageKindNotice,
	event.MsgEmote:  api.MessageKindEmote,
}

// message converts a stored m.room.message event.
func (c *Core) message(ctx context.Context, evt *event.Event) (api.Message, error) {
	content := evt.Content.AsMessage()
	kind, ok := messageKinds[content.MsgType]
	if !ok {
		kind = api.MessageKindUnsupported
	}
	msg := api.Message{
		MessageID:      messageID(evt.RoomID, evt.ID),
		ConversationID: conversationID(evt.RoomID),
		FromMe:         evt.Sender == c.host.Matrix.UserID(),
		TimestampMS:    evt.Timestamp,
		Kind:           kind,
		Text:           content.Body,
		Status:         api.MessageStatusReceived,
	}
	member, err := c.host.Matrix.State(ctx, evt.RoomID, event.StateMember, string(evt.Sender))
	if err != nil {
		return msg, err
	} else if member != nil {
		msg.SenderName = member.Content.AsMember().Displayname
	}
	if !msg.FromMe {
		return msg, nil
	}
	// Messages from the user's other devices have no status here: the
	// network delivered them already.
	msg.Status = api.MessageStatusSent
	st, err := c.host.Matrix.MessageStatus(ctx, evt.ID)
	if err != nil || st == nil {
		return msg, err
	}
	switch st.Status {
	case event.MessageStatusPending:
		msg.Status = api.MessageStatusSending
	case event.MessageStatusRetriable, event.MessageStatusFail:
		msg.Status = api.MessageStatusFailed
		msg.Error = st.Message
	}
	return msg, nil
}

// stats reports the Go runtime's memory after a garbage collection, for the
// leak tests of the embedding application.
func (c *Core) stats(context.Context, api.Empty) (api.StatsResult, error) {
	runtime.GC()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return api.StatsResult{
		HeapAllocBytes: int64(mem.HeapAlloc), //nolint:gosec // A heap of 2^63 bytes does not exist.
		HeapSysBytes:   int64(mem.HeapSys),   //nolint:gosec // Same.
		Goroutines:     int64(runtime.NumGoroutine()),
	}, nil
}

func (c *Core) ping(_ context.Context, p api.PingPayload) (api.PingPayload, error) {
	return p, nil
}

// loginName returns the remote name of a login, if bridgev2 has it loaded.
func loginName(br *bridgev2.Bridge, login networkid.UserLoginID) string {
	if ul := br.GetCachedUserLoginByID(login); ul != nil {
		return ul.RemoteName
	}
	return ""
}
