// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/localmatrix"
	"github.com/quentinemusee/musubee/core/persons"
)

// This file translates the bridges and the local Matrix storage into the
// API's domain objects. No Matrix type crosses it towards the API.

const (
	defaultMessageLimit  = 50
	maxMessageLimit      = 200
	maxConversationLimit = 200
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
		result.Networks = append(result.Networks, wordings[network].network(n))
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

func (c *Core) accountsLogout(ctx context.Context, p api.AccountsLogoutParams) (api.Empty, error) {
	network, loginID, err := parseAccountID(p.AccountID)
	if err != nil {
		return api.Empty{}, err
	}
	var login *bridgev2.UserLogin
	if br := c.host.Bridge(network); br != nil && c.hasNetwork(network) {
		login = br.GetCachedUserLoginByID(loginID)
	}
	if login == nil {
		return api.Empty{}, newError(api.ErrorCodeNotFound, "no account %q", p.AccountID)
	}
	// What UserLogin.Logout does, but blocking: when the command returns,
	// the account's conversations are gone too.
	login.Delete(ctx, status.BridgeState{StateEvent: status.StateLoggedOut}, bridgev2.DeleteOpts{LogoutRemote: true, BlockingCleanup: true})
	return api.Empty{}, nil
}

func (c *Core) account(ctx context.Context, network networkid.BridgeID, login networkid.UserLoginID, name string) (api.Account, error) {
	// Every network's bridge runs in this core: hosted bridges are to come
	// (CLAUDE.md §2).
	account := api.Account{
		AccountID: accountID(network, login), NetworkID: string(network), Name: name,
		State: api.AccountStateConnecting, Connection: api.AccountConnectionOnDevice,
	}
	state, err := c.host.Matrix.BridgeState(ctx, string(network), string(login))
	if err != nil || state == nil {
		return account, err
	}
	if br := c.host.Bridge(network); br != nil {
		if ul := br.GetCachedUserLoginByID(login); ul != nil {
			account.Verification = verification(ul.Client)
		}
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
	if p.Limit < 0 || p.Limit > maxConversationLimit {
		return result, newError(api.ErrorCodeInvalidParams, "limit must be between 1 and %d", maxConversationLimit)
	}
	var after *localmatrix.ActivityCursor
	if p.Before != "" {
		cur, err := parseActivityCursor(p.Before)
		if err != nil {
			return result, err
		}
		after = &cur
	}
	// One more than asked tells whether more rooms remain.
	limit := int(p.Limit)
	if limit > 0 {
		limit++
	}
	rooms, err := c.host.Matrix.RoomsByActivity(ctx, after, limit)
	if err != nil {
		return result, err
	}
	if p.Limit > 0 && len(rooms) > int(p.Limit) {
		rooms = rooms[:p.Limit]
		result.Before = activityCursor(rooms[len(rooms)-1].Cursor())
	}
	// Pinned conversations come first, on the first page only (API 1.24).
	if rooms, err = c.pinnedFirst(ctx, rooms, p.Before == ""); err != nil {
		return result, err
	}
	owners, err := c.persons.Owners(ctx)
	if err != nil {
		return result, err
	}
	for _, room := range rooms {
		conv, err := c.conversation(ctx, room, owners)
		if err != nil {
			return result, err
		}
		if conv != nil && (p.AccountID == "" || conv.AccountID == p.AccountID) {
			result.Conversations = append(result.Conversations, *conv)
		}
	}
	return result, nil
}

// conversationOf returns the conversation of a room, as conversation does,
// reading the room's activity first.
func (c *Core) conversationOf(ctx context.Context, roomID id.RoomID, owners map[persons.Key]string) (*api.Conversation, error) {
	activity, err := c.host.Matrix.RoomActivity(ctx, roomID)
	if err != nil {
		return nil, err
	}
	return c.conversation(ctx, activity, owners)
}

// conversation returns the conversation of a room, or nil while the room is
// not complete yet: bridgev2 names the room and records it as a portal a
// moment after creating it. owners maps the linked conversations to their
// persons (persons.Store.Owners); if nil, the person is read from the store.
func (c *Core) conversation(ctx context.Context, activity *localmatrix.RoomActivity, owners map[persons.Key]string) (*api.Conversation, error) {
	room := activity.Room
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
	receiver, login, err := portalLogin(ctx, br, portal)
	if err != nil || receiver == "" {
		return nil, err
	}
	kind := api.ConversationKindGroup
	if portal.RoomType == database.RoomTypeDM {
		kind = api.ConversationKindDirect
	}
	key := portalKey(br, portal)
	personID := owners[key]
	if owners == nil {
		if personID, err = c.persons.Owner(ctx, key); err != nil {
			return nil, err
		}
	}
	conv := &api.Conversation{
		ConversationID: conversationID(room.ID),
		AccountID:      accountID(br.ID, receiver),
		NetworkID:      string(br.ID),
		Name:           name.Content.AsRoomName().Name,
		Kind:           kind,
		PersonID:       personID,
		ActivityMS:     activity.At,
		Actions:        actions(ctx, portal, login),
		Privacy:        privacy(ctx, br, portal, login),
	}
	if activity.LastMessage != nil {
		msg, err := c.messageOf(ctx, activity.LastMessage)
		if err != nil {
			return nil, err
		}
		conv.LastMessage = &msg
	}
	read, err := c.host.Matrix.ActivityReadState(ctx, activity)
	if err != nil {
		return nil, err
	}
	conv.UnreadCount = int64(read.Unread)
	conv.MarkedUnread = read.MarkedUnread
	conv.MutedUntilMS = mutedUntil(read.MutedUntil, time.Now())
	conv.Pinned = read.Pinned
	if conv.AvatarID, err = c.avatarID(ctx, room.ID); err != nil {
		return nil, err
	}
	conv.Archived = read.ArchivedAt != 0
	if conv.Draft, err = c.host.Matrix.Draft(ctx, room.ID); err != nil {
		return nil, err
	}
	if conv.Seen, err = c.seen(ctx, room.ID); err != nil {
		return nil, err
	}
	return conv, nil
}

// seen says which of the user's messages others read, and who (API 1.21).
func (c *Core) seen(ctx context.Context, roomID id.RoomID) (*api.ConversationSeen, error) {
	seen, err := c.host.Matrix.Seen(ctx, roomID)
	if err != nil || seen == nil {
		return nil, err
	}
	result := &api.ConversationSeen{MessageID: messageID(roomID, seen.Message.ID), Names: []string{}}
	for _, reader := range seen.Readers {
		name, err := c.memberName(ctx, roomID, reader)
		if err != nil {
			return nil, err
		}
		result.Names = append(result.Names, name)
	}
	return result, nil
}

// portalLogin returns the ID of the user's login that a portal belongs to,
// or "" while no login reaches it, and that login while it is loaded.
func portalLogin(ctx context.Context, br *bridgev2.Bridge, portal *bridgev2.Portal) (networkid.UserLoginID, *bridgev2.UserLogin, error) {
	if portal.Receiver != "" {
		login, err := br.GetExistingUserLoginByID(ctx, portal.Receiver)
		return portal.Receiver, login, err
	}
	// A portal shared by the user's logins: any of them reaches it.
	logins, err := br.GetUserLoginsInPortal(ctx, portal.PortalKey)
	if err != nil || len(logins) == 0 {
		return "", nil, err
	}
	return logins[0].ID, logins[0], nil
}

// pinnedFirst takes the pinned rooms out of a page of rooms by activity,
// and puts all of them before it on the first page (docs/ADR/0052).
func (c *Core) pinnedFirst(ctx context.Context, rooms []*localmatrix.RoomActivity, first bool) ([]*localmatrix.RoomActivity, error) {
	pinned, err := c.host.Matrix.PinnedRooms(ctx)
	if err != nil || len(pinned) == 0 {
		return rooms, err
	}
	isPinned := make(map[id.RoomID]bool, len(pinned))
	for _, room := range pinned {
		isPinned[room.Room.ID] = true
	}
	var result []*localmatrix.RoomActivity
	if first {
		result = append(result, pinned...)
	}
	for _, room := range rooms {
		if !isPinned[room.Room.ID] {
			result = append(result, room)
		}
	}
	return result, nil
}

func (c *Core) conversationsTyping(ctx context.Context, p api.ConversationsTypingParams) (api.Empty, error) {
	roomID, err := c.room(ctx, p.ConversationID)
	if err != nil {
		return api.Empty{}, err
	}
	return api.Empty{}, c.host.Matrix.SendTyping(ctx, roomID, p.Typing)
}

func (c *Core) conversationsMarkUnread(ctx context.Context, p api.ConversationsMarkUnreadParams) (api.Empty, error) {
	roomID, err := c.room(ctx, p.ConversationID)
	if err != nil {
		return api.Empty{}, err
	}
	return api.Empty{}, c.host.Matrix.MarkUnread(ctx, roomID)
}

// conversationsMute mutes or unmutes a conversation (API 1.23,
// docs/ADR/0050).
func (c *Core) conversationsMute(ctx context.Context, p api.ConversationsMuteParams) (api.Empty, error) {
	if p.UntilMS < localmatrix.MutedForever {
		return api.Empty{}, newError(api.ErrorCodeInvalidParams, "until_ms must be a time, -1 or 0")
	}
	roomID, err := c.room(ctx, p.ConversationID)
	if err != nil {
		return api.Empty{}, err
	}
	return api.Empty{}, c.host.Matrix.Mute(ctx, roomID, p.UntilMS)
}

// avatarID is the media ID of a room's picture (API 1.32, docs/ADR/0061),
// or "": a direct conversation's is its contact's (bridgev2 copies it with
// PrivateChatPortalMeta).
func (c *Core) avatarID(ctx context.Context, roomID id.RoomID) (string, error) {
	avatar, err := c.host.Matrix.State(ctx, roomID, event.StateRoomAvatar, "")
	if err != nil || avatar == nil {
		return "", err
	}
	url := avatar.Content.AsRoomAvatar().URL
	if url == "" {
		return "", nil
	}
	return mediaID(url), nil
}

// conversationsSetDraft keeps the text left unsent in a conversation
// (API 1.30, docs/ADR/0057).
func (c *Core) conversationsSetDraft(ctx context.Context, p api.ConversationsSetDraftParams) (api.Empty, error) {
	roomID, err := c.room(ctx, p.ConversationID)
	if err != nil {
		return api.Empty{}, err
	}
	return api.Empty{}, c.host.Matrix.SetDraft(ctx, roomID, p.Text)
}

// conversationsPin pins or unpins a conversation (API 1.24,
// docs/ADR/0052).
func (c *Core) conversationsPin(ctx context.Context, p api.ConversationsPinParams) (api.Empty, error) {
	roomID, err := c.room(ctx, p.ConversationID)
	if err != nil {
		return api.Empty{}, err
	}
	return api.Empty{}, c.host.Matrix.Pin(ctx, roomID, p.Pinned)
}

// mutedUntil is a conversation's muted_until_ms: a mute that ended is
// none.
func mutedUntil(untilMS int64, now time.Time) int64 {
	if untilMS > 0 && untilMS <= now.UnixMilli() {
		return 0
	}
	return untilMS
}

func (c *Core) conversationsMarkRead(ctx context.Context, p api.ConversationsMarkReadParams) (api.Empty, error) {
	roomID, err := c.room(ctx, p.ConversationID)
	if err != nil {
		return api.Empty{}, err
	}
	var eventID id.EventID
	if p.MessageID != "" {
		msgRoom, evt, err := parseMessageID(p.MessageID)
		if err != nil {
			return api.Empty{}, err
		}
		if msgRoom != roomID {
			return api.Empty{}, newError(api.ErrorCodeNotFound, "no message %q in conversation %q", p.MessageID, p.ConversationID)
		}
		eventID = evt
	} else {
		last, err := c.host.Matrix.MessagesBefore(ctx, roomID, 0, 1)
		if err != nil {
			return api.Empty{}, err
		}
		if len(last) == 0 {
			// Nothing to read; a "marked unread" flag still goes.
			return api.Empty{}, c.host.Matrix.ClearMarkedUnread(ctx, roomID)
		}
		eventID = last[0].ID
	}
	err = c.host.Matrix.MarkRead(ctx, roomID, eventID)
	if errors.Is(err, localmatrix.ErrNotFound) {
		return api.Empty{}, newError(api.ErrorCodeNotFound, "no message %q", p.MessageID)
	}
	return api.Empty{}, err
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
	result.Messages, err = c.messages(ctx, roomID, events)
	return result, err
}

func (c *Core) messagesSend(ctx context.Context, p api.MessagesSendParams) (api.MessagesSendResult, error) {
	if p.Text == "" {
		return api.MessagesSendResult{}, newError(api.ErrorCodeInvalidParams, "text is empty")
	}
	roomID, err := c.room(ctx, p.ConversationID)
	if err != nil {
		return api.MessagesSendResult{}, err
	}
	content := &event.MessageEventContent{MsgType: event.MsgText, Body: p.Text}
	if p.ReplyTo != "" {
		_, target, err := c.target(ctx, p.ConversationID, p.ReplyTo)
		if err != nil {
			return api.MessagesSendResult{}, err
		}
		if target.Unsigned.RedactedBecause != nil {
			return api.MessagesSendResult{}, newError(api.ErrorCodeInvalidParams, "cannot answer message %q", p.ReplyTo)
		}
		if err = c.allowed(ctx, roomID, "replies", func(a *api.ConversationActions) bool { return a.Reply }); err != nil {
			return api.MessagesSendResult{}, err
		}
		content.RelatesTo = (&event.RelatesTo{}).SetReplyTo(target.ID)
	}
	eventID, err := c.sendOrQueue(ctx, roomID, content)
	if err != nil {
		return api.MessagesSendResult{}, err
	}
	evt, err := c.host.Matrix.Event(ctx, roomID, eventID)
	if err != nil {
		return api.MessagesSendResult{}, err
	}
	msg, err := c.messageOf(ctx, evt)
	return api.MessagesSendResult{Message: msg}, err
}

// mentions says whether a message's content mentions a user, as its
// network marked it (Matrix's m.mentions, docs/ADR/0055): by ID, or the
// whole room.
func mentions(content *event.MessageEventContent, userID id.UserID) bool {
	if content == nil || content.Mentions == nil {
		return false
	}
	return content.Mentions.Room || slices.Contains(content.Mentions.UserIDs, userID)
}

var messageKinds = map[event.MessageType]api.MessageKind{
	event.MsgText:   api.MessageKindText,
	event.MsgNotice: api.MessageKindNotice,
	event.MsgEmote:  api.MessageKindEmote,
}

// messages converts stored m.room.message events of a room (not edits),
// with what their edits, reactions and redactions changed.
func (c *Core) messages(ctx context.Context, roomID id.RoomID, events []*event.Event) ([]api.Message, error) {
	relations, err := c.host.Matrix.Relations(ctx, roomID, events)
	if err != nil {
		return nil, err
	}
	messages := make([]api.Message, 0, len(events))
	for _, evt := range events {
		msg, err := c.message(ctx, evt, relations[evt.ID])
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

// messageOf converts one stored m.room.message event (not an edit).
func (c *Core) messageOf(ctx context.Context, evt *event.Event) (api.Message, error) {
	messages, err := c.messages(ctx, evt.RoomID, []*event.Event{evt})
	if err != nil {
		return api.Message{}, err
	}
	return messages[0], nil
}

// latestContent returns a message's content as its latest edit left it,
// and whether it was edited.
func latestContent(evt *event.Event, rel *localmatrix.Relations) (*event.MessageEventContent, bool) {
	if rel != nil && rel.Edit != nil {
		if edited := rel.Edit.Content.AsMessage().NewContent; edited != nil {
			return edited, true
		}
	}
	return evt.Content.AsMessage(), false
}

func (c *Core) senderName(ctx context.Context, evt *event.Event) (string, error) {
	member, err := c.host.Matrix.State(ctx, evt.RoomID, event.StateMember, string(evt.Sender))
	if err != nil || member == nil {
		return "", err
	}
	return member.Content.AsMember().Displayname, nil
}

// message converts a stored m.room.message event (not an edit), given its
// relations (nil without any).
func (c *Core) message(ctx context.Context, evt *event.Event, rel *localmatrix.Relations) (api.Message, error) {
	msg := api.Message{
		MessageID:      messageID(evt.RoomID, evt.ID),
		ConversationID: conversationID(evt.RoomID),
		FromMe:         evt.Sender == c.host.Matrix.UserID(),
		TimestampMS:    evt.Timestamp,
		Kind:           api.MessageKindText,
		Status:         api.MessageStatusReceived,
	}
	var err error
	if msg.SenderName, err = c.senderName(ctx, evt); err != nil {
		return msg, err
	}
	if !msg.FromMe {
		// The sender's current picture (API 1.33): profiles are global and
		// kept up to date, member events are not rewritten.
		profile, err := c.host.Matrix.Profile(ctx, evt.Sender)
		if err != nil {
			return msg, err
		}
		if profile.AvatarURL != "" {
			msg.SenderAvatarID = mediaID(profile.AvatarURL)
		}
	}
	if evt.Unsigned.RedactedBecause != nil {
		msg.Deleted = true
	} else {
		content, edited := latestContent(evt, rel)
		msg.Kind, msg.Text, msg.Media = messageContent(evt.Content.AsMessage(), content)
		msg.Edited = edited
		msg.MentionsMe = !msg.FromMe && mentions(content, c.host.Matrix.UserID())
		if rel != nil {
			if msg.Reactions, err = c.reactions(ctx, evt.RoomID, rel.Reactions); err != nil {
				return msg, err
			}
		}
		// The reply is the original's: an edit's new content has none.
		if replyTo := evt.Content.AsMessage().RelatesTo.GetNonFallbackReplyTo(); replyTo != "" {
			if msg.ReplyTo, err = c.reply(ctx, evt.RoomID, replyTo); err != nil {
				return msg, err
			}
		}
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
		if st.Reason == localmatrix.ReasonQueued {
			msg.Status = api.MessageStatusQueued
		}
	case event.MessageStatusRetriable, event.MessageStatusFail:
		msg.Status = api.MessageStatusFailed
		msg.Error = st.Message
	}
	return msg, nil
}

// maxReactionNames bounds the names given per reaction (API 1.19).
const maxReactionNames = 10

func (c *Core) reactions(ctx context.Context, roomID id.RoomID, reactions []localmatrix.Reaction) ([]api.MessageReaction, error) {
	var out []api.MessageReaction
	for _, r := range reactions {
		reaction := api.MessageReaction{
			Key:    r.Key,
			Count:  int64(len(r.Senders)),
			FromMe: slices.Contains(r.Senders, c.host.Matrix.UserID()),
		}
		for _, sender := range r.Senders {
			if sender == c.host.Matrix.UserID() || len(reaction.Names) == maxReactionNames {
				continue
			}
			name, err := c.memberName(ctx, roomID, sender)
			if err != nil {
				return nil, err
			}
			if name != "" {
				reaction.Names = append(reaction.Names, name)
			}
		}
		out = append(out, reaction)
	}
	return out, nil
}

// memberName returns a room member's display name, or "".
func (c *Core) memberName(ctx context.Context, roomID id.RoomID, userID id.UserID) (string, error) {
	member, err := c.host.Matrix.State(ctx, roomID, event.StateMember, string(userID))
	if err != nil || member == nil {
		return "", err
	}
	return member.Content.AsMember().Displayname, nil
}

// reply describes the message that another answers, which may not be
// stored.
func (c *Core) reply(ctx context.Context, roomID id.RoomID, eventID id.EventID) (*api.MessageReply, error) {
	reply := &api.MessageReply{MessageID: messageID(roomID, eventID)}
	evt, err := c.host.Matrix.Event(ctx, roomID, eventID)
	if errors.Is(err, localmatrix.ErrNotFound) {
		return reply, nil
	} else if err != nil {
		return nil, err
	}
	reply.FromMe = evt.Sender == c.host.Matrix.UserID()
	if reply.SenderName, err = c.senderName(ctx, evt); err != nil {
		return nil, err
	}
	if evt.Unsigned.RedactedBecause != nil {
		reply.Deleted = true
		return reply, nil
	}
	relations, err := c.host.Matrix.Relations(ctx, roomID, []*event.Event{evt})
	if err != nil {
		return nil, err
	}
	content, _ := latestContent(evt, relations[eventID])
	if kind, text, _ := messageContent(evt.Content.AsMessage(), content); kind != api.MessageKindText {
		reply.Kind, reply.Text = kind, text
	} else {
		reply.Text = text
	}
	return reply, nil
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
