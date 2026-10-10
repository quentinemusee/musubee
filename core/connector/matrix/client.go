// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package matrix

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/ptr"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/crypto/cryptohelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

const (
	// timelineLimit is how many recent messages per room the first sync
	// brings: the history of a conversation added with the account.
	timelineLimit = 20
	// retryDelay is the wait before connecting again after a failure.
	retryDelay = 10 * time.Second
	// undecryptable replaces a message the core could not decrypt: the keys
	// never came (an old message, or a sender that does not share its keys
	// with new devices).
	undecryptable = "This message could not be decrypted on this device."
)

// errNotReady is returned when a message is sent before the client is
// connected. Like every error a connector returns, it carries a reason and a
// message (see the echo connector).
var errNotReady = bridgev2.WrapErrorInStatus(errors.New("the Matrix account is not connected yet")).
	WithErrorReason(event.MessageStatusNetworkError).
	WithErrorAsMessage().
	WithSendNotice(false)

// client is the bridgev2.NetworkAPI of one Matrix account: a Matrix client
// of the account's homeserver, whose rooms it mirrors as portals.
type client struct {
	connector *Connector
	login     *bridgev2.UserLogin
	cli       *mautrix.Client
	crypto    *cryptohelper.CryptoHelper
	store     *crypto.SQLCryptoStore
	self      id.UserID

	lock      sync.Mutex
	connected bool
	// ready is set once the encryption is initialized: messages can be sent.
	ready       bool
	initialized bool
	cancel      context.CancelFunc
	done        chan struct{}
	// direct maps the direct rooms to their other member (m.direct).
	direct  map[id.RoomID]id.UserID
	directs event.DirectChatsEventContent

	// ghostLock serializes the updates of the ghosts' profiles: the sync
	// and the decryption of late messages both update them (see
	// updateGhost).
	ghostLock sync.Mutex
	names     map[id.UserID]string

	// Fields of the sync goroutine only.
	healthy bool
	joining map[id.RoomID]bool
}

var _ bridgev2.NetworkAPI = (*client)(nil)

// cryptoAccountID keys a login's rows in the crypto store. It names the
// device too: signing in again creates a new device, whose keys must not
// mix with the old one's.
func cryptoAccountID(user id.UserID, device id.DeviceID) string {
	return string(user) + "/" + string(device)
}

func newClient(c *Connector, login *bridgev2.UserLogin, meta *LoginMetadata) (*client, error) {
	self := id.UserID(login.ID)
	cli, err := mautrix.NewClient(meta.HomeserverURL, self, meta.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("creating the Matrix client: %w", err)
	}
	cli.DeviceID = id.DeviceID(meta.DeviceID)
	cli.Log = login.Log.With().Str("component", "matrix_client").Logger()
	cli.StateStore = c.stateStore
	store := c.cryptoStore(cryptoAccountID(self, cli.DeviceID), meta.DeviceID)
	cli.Store = store
	syncer := &syncer{DefaultSyncer: mautrix.NewDefaultSyncer()}
	syncer.FilterJSON = &mautrix.Filter{Room: &mautrix.RoomFilter{Timeline: &mautrix.FilterPart{Limit: timelineLimit}}}
	cli.Syncer = syncer
	helper, err := cryptohelper.NewCryptoHelper(cli, c.Config.PickleKey, store)
	if err != nil {
		return nil, fmt.Errorf("creating the encryption helper: %w", err)
	}
	cl := &client{
		connector: c,
		login:     login,
		cli:       cli,
		crypto:    helper,
		store:     store,
		self:      self,
		direct:    map[id.RoomID]id.UserID{},
		names:     map[id.UserID]string{},
		joining:   map[id.RoomID]bool{},
	}
	syncer.client = cl
	helper.DecryptErrorCallback = cl.onDecryptError
	// The crypto helper keeps the room state up to date only with the stores
	// it creates itself; the connector's are shared, so it does it here. The
	// global listeners run before the typed ones: the state is up to date
	// when the handlers below read it.
	syncer.OnEvent(cli.StateStoreSyncHandler)
	syncer.OnSync(cl.onSync)
	syncer.OnEventType(event.EventMessage, cl.onMessage)
	syncer.OnEventType(event.StateMember, cl.onMember)
	syncer.OnEventType(event.AccountDataDirectChats, cl.onDirectChats)
	return cl, nil
}

// syncer reports the sync's failures as the login's state.
type syncer struct {
	*mautrix.DefaultSyncer
	client *client
}

func (s *syncer) OnFailedSync(res *mautrix.RespSync, err error) (time.Duration, error) {
	if errors.Is(err, mautrix.MUnknownToken) {
		return 0, err
	}
	s.client.healthy = false
	s.client.login.BridgeState.Send(status.BridgeState{StateEvent: status.StateTransientDisconnect, Error: "matrix-sync-failed"})
	return retryDelay, nil
}

func (c *client) portalKey(room id.RoomID) networkid.PortalKey {
	return networkid.PortalKey{ID: networkid.PortalID(room), Receiver: c.login.ID}
}

// Connect starts the client in the background: the encryption, then the
// sync, until Disconnect.
func (c *client) Connect(context.Context) {
	c.lock.Lock()
	defer c.lock.Unlock()
	// A connection started in the background may run once the bridge is
	// stopping, after Disconnect: it must not reconnect.
	if c.connected || c.connector.br.IsStopping() {
		return
	}
	c.connected = true
	var runCtx context.Context
	runCtx, c.cancel = context.WithCancel(c.login.Log.WithContext(c.connector.br.BackgroundCtx))
	c.done = make(chan struct{})
	c.login.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnecting})
	go c.run(runCtx, c.done)
}

func (c *client) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for !c.setUp(ctx) {
		if !c.wait(ctx) {
			return
		}
	}
	for {
		err := c.cli.SyncWithContext(ctx)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, mautrix.MUnknownToken) {
			c.badCredentials()
			return
		}
		zerolog.Ctx(ctx).Err(err).Msg("Matrix sync stopped")
		c.healthy = false
		c.login.BridgeState.Send(status.BridgeState{StateEvent: status.StateTransientDisconnect, Error: "matrix-sync-failed"})
		if !c.wait(ctx) {
			return
		}
	}
}

// setUp initializes the encryption and reads the direct rooms, once. It
// reports whether the sync can start.
func (c *client) setUp(ctx context.Context) bool {
	c.lock.Lock()
	initialized := c.initialized
	c.lock.Unlock()
	if !initialized {
		if err := c.crypto.Init(ctx); err != nil {
			if errors.Is(err, mautrix.MUnknownToken) {
				c.badCredentials()
				return false
			}
			zerolog.Ctx(ctx).Err(err).Msg("Failed to initialize the Matrix encryption")
			c.login.BridgeState.Send(status.BridgeState{StateEvent: status.StateTransientDisconnect, Error: "matrix-crypto-init-failed"})
			return false
		}
		c.lock.Lock()
		c.cli.Crypto = c.crypto
		c.initialized = true
		c.ready = true
		c.lock.Unlock()
	}
	var directs event.DirectChatsEventContent
	err := c.cli.GetAccountData(ctx, event.AccountDataDirectChats.Type, &directs)
	if err != nil && !errors.Is(err, mautrix.MNotFound) {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to read the direct rooms")
		c.login.BridgeState.Send(status.BridgeState{StateEvent: status.StateTransientDisconnect, Error: "matrix-direct-failed"})
		return false
	}
	c.setDirects(directs)
	return true
}

// wait waits before trying again; false if the client is disconnecting.
func (c *client) wait(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(retryDelay):
		return true
	}
}

func (c *client) badCredentials() {
	c.login.BridgeState.Send(status.BridgeState{
		StateEvent: status.StateBadCredentials,
		Error:      "matrix-session-ended",
		Message:    "The session was ended on the server: sign in again.",
	})
}

// Disconnect stops the sync and waits for it.
func (c *client) Disconnect() {
	c.lock.Lock()
	cancel, done := c.cancel, c.done
	c.connected = false
	c.cancel, c.done = nil, nil
	c.lock.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	c.cli.StopSync()
	<-done
}

func (c *client) IsLoggedIn() bool {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.connected
}

// LogoutRemote ends the session on the homeserver, which deletes the
// device, and forgets the device's keys. bridgev2 does not call Disconnect
// before it.
func (c *client) LogoutRemote(ctx context.Context) {
	c.endSession(ctx)
}

// endSession disconnects, logs the device out and deletes its keys.
func (c *client) endSession(ctx context.Context) {
	c.Disconnect()
	if _, err := c.cli.Logout(ctx); err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Msg("Failed to log out of the homeserver")
	}
	if err := c.connector.deleteKeys(ctx, c.store.AccountID); err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to delete the device's keys")
	}
}

func (c *client) IsThisUser(_ context.Context, user networkid.UserID) bool {
	return user == networkid.UserID(c.self)
}

// setDirects replaces the direct rooms.
func (c *client) setDirects(directs event.DirectChatsEventContent) (changed []id.RoomID) {
	direct := map[id.RoomID]id.UserID{}
	for user, rooms := range directs {
		for _, room := range rooms {
			direct[room] = user
		}
	}
	c.lock.Lock()
	defer c.lock.Unlock()
	for room, user := range direct {
		if c.direct[room] != user {
			changed = append(changed, room)
		}
	}
	for room := range c.direct {
		if _, ok := direct[room]; !ok {
			changed = append(changed, room)
		}
	}
	c.direct, c.directs = direct, directs
	return changed
}

func (c *client) directWith(room id.RoomID) (id.UserID, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()
	user, ok := c.direct[room]
	return user, ok
}

// onSync runs before the events of a sync response are handled: it
// announces the rooms that are new or whose name or members changed, so
// that their portals exist before their messages arrive.
func (c *client) onSync(ctx context.Context, res *mautrix.RespSync, _ string) bool {
	for room, data := range res.Rooms.Join {
		if c.needsResync(ctx, room, data) {
			c.resync(ctx, room)
		}
	}
	for room := range res.Rooms.Leave {
		if c.hasPortal(ctx, room) {
			c.login.QueueRemoteEvent(&simplevent.ChatDelete{
				EventMeta: simplevent.EventMeta{Type: bridgev2.RemoteEventChatDelete, PortalKey: c.portalKey(room)},
				OnlyForMe: true,
			})
		}
	}
	if !c.healthy {
		c.healthy = true
		c.login.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
	}
	return true
}

func (c *client) hasPortal(ctx context.Context, room id.RoomID) bool {
	portal, err := c.connector.br.GetExistingPortalByKey(ctx, c.portalKey(room))
	return err == nil && portal != nil && portal.MXID != ""
}

func (c *client) needsResync(ctx context.Context, room id.RoomID, data *mautrix.SyncJoinedRoom) bool {
	if !c.hasPortal(ctx, room) {
		return true
	}
	changes := func(events []*event.Event) bool {
		for _, evt := range events {
			if evt.StateKey != nil && (evt.Type.Type == event.StateRoomName.Type || evt.Type.Type == event.StateMember.Type) {
				return true
			}
		}
		return false
	}
	return changes(data.State.Events) || changes(data.Timeline.Events) || (data.StateAfter != nil && changes(data.StateAfter.Events))
}

// resync announces a room with its current state. The ghosts of its members
// get their names first, one at a time: bridgev2 creates portals in
// parallel, and a ghost shared by several rooms must not be updated by
// several at once (see the echo connector's updateSelfGhost).
func (c *client) resync(ctx context.Context, room id.RoomID) {
	info, members, err := c.chatInfo(ctx, room)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Stringer("room_id", room).Msg("Failed to read a room's state")
		return
	}
	for _, m := range members {
		c.updateGhost(ctx, m.user, m.displayName, true)
	}
	c.login.QueueRemoteEvent(&simplevent.ChatResync{
		EventMeta: simplevent.EventMeta{
			Type:         bridgev2.RemoteEventChatResync,
			PortalKey:    c.portalKey(room),
			CreatePortal: true,
		},
		ChatInfo: info,
	})
}

// chatInfo reads a room's state from the homeserver.
func (c *client) chatInfo(ctx context.Context, room id.RoomID) (*bridgev2.ChatInfo, []member, error) {
	state, err := c.cli.State(ctx, room)
	if err != nil {
		return nil, nil, err
	}
	name := ""
	if evt := state[event.StateRoomName][""]; evt != nil {
		name = evt.Content.AsRoomName().Name
	}
	var members []member
	memberMap := bridgev2.ChatMemberMap{}
	for key, evt := range state[event.StateMember] {
		content := evt.Content.AsMember()
		if content.Membership != event.MembershipJoin && content.Membership != event.MembershipInvite {
			continue
		}
		user := id.UserID(key)
		members = append(members, member{user: user, displayName: content.Displayname})
		memberMap[networkid.UserID(user)] = bridgev2.ChatMember{
			EventSender: bridgev2.EventSender{Sender: networkid.UserID(user), IsFromMe: user == c.self},
			Membership:  content.Membership,
		}
	}
	sortMembers(members)
	info := &bridgev2.ChatInfo{
		Name:    ptr.Ptr(roomName(name, c.self, members)),
		Type:    ptr.Ptr(database.RoomTypeDefault),
		Members: &bridgev2.ChatMemberList{IsFull: true, MemberMap: memberMap},
	}
	if other, ok := c.directWith(room); ok {
		info.Type = ptr.Ptr(database.RoomTypeDM)
		info.Members.OtherUserID = networkid.UserID(other)
	}
	return info, members, nil
}

// updateGhost gives a ghost the name of its Matrix user. Unless force is
// set, it only names ghosts that have no name yet: the name of a member in
// one room should not overwrite the name it has elsewhere at every message.
func (c *client) updateGhost(ctx context.Context, user id.UserID, displayName string, force bool) {
	c.ghostLock.Lock()
	defer c.ghostLock.Unlock()
	name := personName(user, displayName)
	if known, ok := c.names[user]; ok && (known == name || !force) {
		return
	}
	ghost, err := c.connector.br.GetGhostByID(ctx, networkid.UserID(user))
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to get a ghost")
		return
	}
	if !force && ghost.Name != "" && ghost.NameSet {
		c.names[user] = ghost.Name
		return
	}
	ghost.UpdateInfo(ctx, &bridgev2.UserInfo{Name: ptr.Ptr(name)})
	c.names[user] = name
}

// senderGhost makes sure the ghost of a message's sender has a name before
// the message is queued (see resync).
func (c *client) senderGhost(ctx context.Context, room id.RoomID, sender id.UserID) {
	displayName := ""
	if m, err := c.connector.stateStore.TryGetMember(ctx, room, sender); err == nil && m != nil {
		displayName = m.Displayname
	}
	c.updateGhost(ctx, sender, displayName, false)
}

// onMember accepts the invitations to direct conversations, and follows the
// renames of the members.
func (c *client) onMember(ctx context.Context, evt *event.Event) {
	content := evt.Content.AsMember()
	source := evt.Mautrix.EventSource
	if source&event.SourceInvite != 0 {
		if id.UserID(evt.GetStateKey()) == c.self && content.Membership == event.MembershipInvite && content.IsDirect {
			c.acceptDirect(ctx, evt.RoomID, evt.Sender)
		}
		return
	}
	if source&event.SourceJoin == 0 || content.Membership != event.MembershipJoin {
		return
	}
	user := id.UserID(evt.GetStateKey())
	c.ghostLock.Lock()
	_, known := c.names[user]
	c.ghostLock.Unlock()
	if known {
		c.updateGhost(ctx, user, content.Displayname, true)
	}
}

// acceptDirect joins a direct conversation the user was invited to, and
// records it as direct, like Matrix clients do when the user accepts.
// Invitations to groups wait: the user accepts them in another client, for
// now (docs/ADR/0018).
func (c *client) acceptDirect(ctx context.Context, room id.RoomID, inviter id.UserID) {
	if c.joining[room] {
		return
	}
	c.joining[room] = true
	if _, err := c.cli.JoinRoomByID(ctx, room); err != nil {
		zerolog.Ctx(ctx).Err(err).Stringer("room_id", room).Msg("Failed to accept an invitation")
		return
	}
	c.lock.Lock()
	directs := event.DirectChatsEventContent{}
	for user, rooms := range c.directs {
		directs[user] = append([]id.RoomID(nil), rooms...)
	}
	c.lock.Unlock()
	directs[inviter] = append(directs[inviter], room)
	if err := c.cli.SetAccountData(ctx, event.AccountDataDirectChats.Type, &directs); err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to record a direct conversation")
	}
	c.setDirects(directs)
}

// onDirectChats follows the changes of the direct rooms made by the user's
// other clients.
func (c *client) onDirectChats(ctx context.Context, evt *event.Event) {
	if evt.Mautrix.EventSource&event.SourceAccountData == 0 || evt.RoomID != "" {
		return
	}
	for _, room := range c.setDirects(*evt.Content.AsDirectChats()) {
		if c.hasPortal(ctx, room) {
			c.resync(ctx, room)
		}
	}
}

// onMessage queues a message of a joined room, received as is or decrypted.
func (c *client) onMessage(ctx context.Context, evt *event.Event) {
	source := evt.Mautrix.EventSource
	if source&event.SourceLeave != 0 || source&(event.SourceTimeline|event.SourceDecrypted) == 0 {
		return
	}
	content := evt.Content.AsMessage()
	// Edits are for a later task: shown as new messages, they would repeat
	// the text.
	if content.RelatesTo != nil && content.RelatesTo.Type == event.RelReplace {
		return
	}
	converted, replyTo := incoming(content)
	c.queueMessage(ctx, evt, converted, replyTo)
}

// onDecryptError replaces a message that could not be decrypted with a
// notice, so that the conversation shows that something was said.
func (c *client) onDecryptError(evt *event.Event, _ error) {
	c.lock.Lock()
	connected := c.connected
	c.lock.Unlock()
	if !connected || evt.RoomID == "" {
		return
	}
	ctx := c.login.Log.WithContext(c.connector.br.BackgroundCtx)
	c.queueMessage(ctx, evt, &event.MessageEventContent{MsgType: event.MsgNotice, Body: undecryptable}, nil)
}

func (c *client) queueMessage(ctx context.Context, evt *event.Event, content *event.MessageEventContent, replyTo *networkid.MessageOptionalPartID) {
	c.senderGhost(ctx, evt.RoomID, evt.Sender)
	c.login.QueueRemoteEvent(&simplevent.Message[*event.MessageEventContent]{
		EventMeta: simplevent.EventMeta{
			Type:         bridgev2.RemoteEventMessage,
			PortalKey:    c.portalKey(evt.RoomID),
			Sender:       bridgev2.EventSender{Sender: networkid.UserID(evt.Sender), IsFromMe: evt.Sender == c.self},
			Timestamp:    time.UnixMilli(evt.Timestamp),
			CreatePortal: true,
		},
		ID:   networkid.MessageID(evt.ID),
		Data: content,
		ConvertMessageFunc: func(context.Context, *bridgev2.Portal, bridgev2.MatrixAPI, *event.MessageEventContent) (*bridgev2.ConvertedMessage, error) {
			return &bridgev2.ConvertedMessage{
				ReplyTo: replyTo,
				Parts:   []*bridgev2.ConvertedMessagePart{{Type: event.EventMessage, Content: content}},
			}, nil
		},
	})
}

// incoming converts a received message: the text, and the message it
// replies to. Media keep their type and caption only, until the media
// support: their content is on the remote server.
func incoming(content *event.MessageEventContent) (*event.MessageEventContent, *networkid.MessageOptionalPartID) {
	clean := *content
	clean.RemoveReplyFallback()
	out := &event.MessageEventContent{MsgType: clean.MsgType, Body: clean.Body}
	if isTextMessage(clean.MsgType) {
		out.Format, out.FormattedBody = clean.Format, clean.FormattedBody
	}
	var replyTo *networkid.MessageOptionalPartID
	if target := content.RelatesTo.GetReplyTo(); target != "" {
		replyTo = &networkid.MessageOptionalPartID{MessageID: networkid.MessageID(target)}
	}
	return out, replyTo
}

func (c *client) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	info, _, err := c.chatInfo(ctx, id.RoomID(portal.ID))
	return info, err
}

// GetUserInfo reads a user's profile on the homeserver.
func (c *client) GetUserInfo(ctx context.Context, ghost *bridgev2.Ghost) (*bridgev2.UserInfo, error) {
	user := id.UserID(ghost.ID)
	displayName := ""
	if profile, err := c.cli.GetDisplayName(ctx, user); err == nil {
		displayName = profile.DisplayName
	}
	return &bridgev2.UserInfo{Name: ptr.Ptr(personName(user, displayName))}, nil
}

// GetCapabilities returns what the conversations support: text, for now.
func (c *client) GetCapabilities(context.Context, *bridgev2.Portal) *event.RoomFeatures {
	return &event.RoomFeatures{
		ID:            "app.musubee.matrix.capabilities.v1",
		MaxTextLength: 32000,
	}
}

// HandleMatrixMessage sends a message the user wrote, encrypted if the room
// is. Its remote event ID becomes the message's ID: the sync brings the
// message back, and bridgev2 recognises it as a duplicate.
func (c *client) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	if !isTextMessage(msg.Content.MsgType) {
		return nil, bridgev2.WrapErrorInStatus(fmt.Errorf("%s messages are not supported yet", msg.Content.MsgType)).
			WithStatus(event.MessageStatusFail).
			WithErrorReason(event.MessageStatusUnsupported).
			WithErrorAsMessage().
			WithIsCertain(true).
			WithSendNotice(false)
	}
	c.lock.Lock()
	ready := c.ready && c.connected
	c.lock.Unlock()
	if !ready {
		return nil, errNotReady
	}
	content := &event.MessageEventContent{
		MsgType:       msg.Content.MsgType,
		Body:          msg.Content.Body,
		Format:        msg.Content.Format,
		FormattedBody: msg.Content.FormattedBody,
	}
	if msg.ReplyTo != nil {
		content.RelatesTo = (&event.RelatesTo{}).SetReplyTo(id.EventID(msg.ReplyTo.ID))
	}
	eventID, err := c.send(ctx, id.RoomID(msg.Portal.ID), string(msg.Event.ID), content)
	if err != nil {
		return nil, bridgev2.WrapErrorInStatus(err).
			WithErrorReason(event.MessageStatusNetworkError).
			WithMessage("The message could not be sent to the Matrix server.").
			WithSendNotice(false)
	}
	return &bridgev2.MatrixMessageResponse{
		DB: &database.Message{
			ID:        networkid.MessageID(eventID),
			SenderID:  networkid.UserID(c.self),
			Timestamp: time.Now(),
		},
	}, nil
}

// send sends a message event, encrypted if the room is. It is
// mautrix.Client.SendMessageEvent, except that the request's body is marked
// sensitive: mautrix logs the body of a failed request, and the text of a
// message must never reach the logs (CLAUDE.md §6).
func (c *client) send(ctx context.Context, room id.RoomID, txnID string, content *event.MessageEventContent) (id.EventID, error) {
	evtType := event.EventMessage
	var body any = content
	encrypted, err := c.cli.StateStore.IsEncrypted(ctx, room)
	if err != nil {
		return "", fmt.Errorf("checking whether the room is encrypted: %w", err)
	}
	if encrypted {
		if body, err = c.crypto.Encrypt(ctx, room, evtType, content); err != nil {
			return "", fmt.Errorf("encrypting the message: %w", err)
		}
		evtType = event.EventEncrypted
	}
	var resp mautrix.RespSendEvent
	_, err = c.cli.MakeFullRequest(ctx, mautrix.FullRequest{
		Method:           http.MethodPut,
		URL:              c.cli.BuildClientURL("v3", "rooms", room, "send", evtType.String(), txnID),
		RequestJSON:      body,
		ResponseJSON:     &resp,
		SensitiveContent: true,
	})
	if err != nil {
		return "", err
	}
	return resp.EventID, nil
}
