// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package localmatrix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Intent is the bridgev2.MatrixAPI of one Matrix user: the bridge bot, the
// ghost of a remote user, or the device's user (double puppet).
type Intent struct {
	server       *Server
	conn         *Connector
	mxid         id.UserID
	doublePuppet bool
}

var _ bridgev2.MatrixAPI = (*Intent)(nil)

// Account data types stored per user and room.
const (
	accountDataReadReceipt = "m.read"
	accountDataTags        = "m.tag"
	accountDataUnread      = "m.marked_unread"
	accountDataMutedUntil  = "com.musubee.muted_until"
)

// ErrEncryptedMediaUnsupported is returned for encrypted attachments: the
// local storage never encrypts media, so no stored media can be encrypted.
var ErrEncryptedMediaUnsupported = errors.New("encrypted media is not supported by the local Matrix connector")

// GetMXID returns the Matrix ID of the user.
func (in *Intent) GetMXID() id.UserID {
	return in.mxid
}

// IsDoublePuppet reports whether this intent acts as the device's user.
func (in *Intent) IsDoublePuppet() bool {
	return in.doublePuppet
}

// transact runs fn in a database transaction and publishes the events it
// stored once the transaction is committed.
func (s *Server) transact(ctx context.Context, fn func(ctx context.Context, store func(*event.Event) error) error) error {
	var stored []*event.Event
	err := s.store.db.DoTxn(ctx, nil, func(ctx context.Context) error {
		return fn(ctx, func(evt *event.Event) error {
			if err := s.store.insertEvent(ctx, evt); err != nil {
				return fmt.Errorf("storing event: %w", err)
			}
			stored = append(stored, evt)
			return nil
		})
	})
	if err != nil {
		return err
	}
	for _, evt := range stored {
		s.publish(Update{Event: evt})
	}
	return nil
}

func (in *Intent) newEvent(roomID id.RoomID, eventType event.Type, stateKey *string, content event.Content, ts time.Time) *event.Event {
	if ts.IsZero() {
		ts = time.Now()
	}
	if stateKey != nil {
		eventType.Class = event.StateEventType
	} else if eventType.Class == event.UnknownEventType {
		eventType.Class = event.MessageEventType
	}
	return &event.Event{
		ID:        newEventID(in.server.serverName),
		RoomID:    roomID,
		Sender:    in.mxid,
		Type:      eventType,
		StateKey:  stateKey,
		Timestamp: ts.UnixMilli(),
		Content:   content,
	}
}

// memberEvent builds the member event of userID, sent by userID itself (a
// join) or by in (an invite or a kick).
func (in *Intent) memberEvent(ctx context.Context, roomID id.RoomID, userID id.UserID, membership event.Membership, isDirect bool) (*event.Event, error) {
	content := &event.MemberEventContent{Membership: membership, IsDirect: isDirect}
	if membership == event.MembershipJoin {
		profile, err := in.server.store.getProfile(ctx, userID)
		if err != nil {
			return nil, err
		}
		content.Displayname = profile.DisplayName
		content.AvatarURL = profile.AvatarURL
	}
	stateKey := userID.String()
	evt := in.newEvent(roomID, event.StateMember, &stateKey, event.Content{Parsed: content}, time.Time{})
	if membership == event.MembershipJoin || membership == event.MembershipLeave && userID == in.mxid {
		evt.Sender = userID
	}
	return evt, nil
}

// ensureJoined makes the user join the room if they are not in it yet, like
// the application service API of a homeserver does before sending.
func (in *Intent) ensureJoined(ctx context.Context, roomID id.RoomID, store func(*event.Event) error) error {
	if _, err := in.server.store.getRoom(ctx, roomID); err != nil {
		return err
	}
	membership, err := in.server.membership(ctx, roomID, in.mxid)
	if err != nil || membership == event.MembershipJoin {
		return err
	} else if membership == event.MembershipBan {
		return fmt.Errorf("%w: %s is banned from %s", ErrForbidden, in.mxid, roomID)
	}
	evt, err := in.memberEvent(ctx, roomID, in.mxid, event.MembershipJoin, false)
	if err != nil {
		return err
	}
	return store(evt)
}

// SendMessage stores a message event sent by the user. A redaction also
// erases the content of the redacted event from the storage.
func (in *Intent) SendMessage(ctx context.Context, roomID id.RoomID, eventType event.Type, content *event.Content, extra *bridgev2.MatrixSendExtra) (*mautrix.RespSendEvent, error) {
	var ts time.Time
	if extra != nil {
		ts = extra.Timestamp
	}
	evt := in.newEvent(roomID, eventType, nil, *content, ts)
	err := in.server.transact(ctx, func(ctx context.Context, store func(*event.Event) error) error {
		if err := in.ensureJoined(ctx, roomID, store); err != nil {
			return err
		}
		if err := store(evt); err != nil {
			return err
		}
		if eventType == event.EventRedaction {
			return in.server.store.redact(ctx, roomID, redactedEventID(evt))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &mautrix.RespSendEvent{EventID: evt.ID}, nil
}

func redactedEventID(evt *event.Event) id.EventID {
	if evt.Redacts != "" {
		return evt.Redacts
	}
	var content event.RedactionEventContent
	if data, err := json.Marshal(&evt.Content); err == nil {
		_ = json.Unmarshal(data, &content)
	}
	return content.Redacts
}

// SendState stores a state event sent by the user.
func (in *Intent) SendState(ctx context.Context, roomID id.RoomID, eventType event.Type, stateKey string, content *event.Content, ts time.Time) (*mautrix.RespSendEvent, error) {
	evt := in.newEvent(roomID, eventType, &stateKey, *content, ts)
	err := in.server.transact(ctx, func(ctx context.Context, store func(*event.Event) error) error {
		isOwnMembership := eventType == event.StateMember && stateKey == in.mxid.String()
		if !isOwnMembership {
			if err := in.ensureJoined(ctx, roomID, store); err != nil {
				return err
			}
		}
		return store(evt)
	})
	if err != nil {
		return nil, err
	}
	return &mautrix.RespSendEvent{EventID: evt.ID}, nil
}

// MarkRead stores the read receipt of the user.
func (in *Intent) MarkRead(ctx context.Context, roomID id.RoomID, eventID id.EventID, ts time.Time) error {
	if ts.IsZero() {
		ts = time.Now()
	}
	return in.server.store.setRoomAccountData(ctx, in.mxid, roomID, accountDataReadReceipt, map[string]any{
		"event_id": eventID,
		"ts":       ts.UnixMilli(),
	})
}

// MarkUnread stores the "marked as unread" flag of the user.
func (in *Intent) MarkUnread(ctx context.Context, roomID id.RoomID, unread bool) error {
	return in.server.store.setRoomAccountData(ctx, in.mxid, roomID, accountDataUnread, &event.MarkedUnreadEventContent{Unread: unread})
}

// MarkTyping does nothing yet: typing notifications are not stored, and the
// application cannot receive them until the core API exposes ephemeral
// updates (T1.4).
func (in *Intent) MarkTyping(context.Context, id.RoomID, bridgev2.TypingType, time.Duration) error {
	return nil
}

func (in *Intent) mediaID(uri id.ContentURIString) (string, error) {
	parsed, err := uri.Parse()
	if err != nil {
		return "", err
	}
	if parsed.Homeserver != in.server.serverName {
		return "", fmt.Errorf("media %s: %w", uri, ErrNotFound)
	}
	return parsed.FileID, nil
}

// DownloadMedia returns stored media.
func (in *Intent) DownloadMedia(ctx context.Context, uri id.ContentURIString, file *event.EncryptedFileInfo) ([]byte, error) {
	if file != nil {
		return nil, ErrEncryptedMediaUnsupported
	}
	mediaID, err := in.mediaID(uri)
	if err != nil {
		return nil, err
	}
	return in.server.store.getMedia(ctx, mediaID)
}

// DownloadMediaToFile writes stored media to a temporary file and passes it
// to the callback. The file is removed afterwards.
func (in *Intent) DownloadMediaToFile(ctx context.Context, uri id.ContentURIString, file *event.EncryptedFileInfo, _ bool, callback func(*os.File) error) error {
	data, err := in.DownloadMedia(ctx, uri, file)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "musubee-media-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return callback(tmp)
}

// UploadMedia stores media and returns its mxc:// URI. Media is never
// encrypted, so the returned file info is always nil.
func (in *Intent) UploadMedia(ctx context.Context, _ id.RoomID, data []byte, fileName, mimeType string) (id.ContentURIString, *event.EncryptedFileInfo, error) {
	mediaID := newMediaID()
	if err := in.server.store.putMedia(ctx, mediaID, mimeType, fileName, data); err != nil {
		return "", nil, err
	}
	return id.ContentURI{Homeserver: in.server.serverName, FileID: mediaID}.CUString(), nil, nil
}

// UploadMediaStream lets the callback write the media to a temporary file,
// then stores it.
func (in *Intent) UploadMediaStream(ctx context.Context, roomID id.RoomID, _ int64, _ bool, cb bridgev2.FileStreamCallback) (id.ContentURIString, *event.EncryptedFileInfo, error) {
	tmp, err := os.CreateTemp("", "musubee-upload-*")
	if err != nil {
		return "", nil, err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	res, err := cb(tmp)
	if err != nil {
		return "", nil, err
	}
	var data []byte
	if res.ReplacementFile != "" {
		data, err = os.ReadFile(res.ReplacementFile)
		_ = os.Remove(res.ReplacementFile)
	} else {
		var buf bytes.Buffer
		if _, err = tmp.Seek(0, io.SeekStart); err == nil {
			_, err = buf.ReadFrom(tmp)
		}
		data = buf.Bytes()
	}
	if err != nil {
		return "", nil, err
	}
	return in.UploadMedia(ctx, roomID, data, res.FileName, res.MimeType)
}

func (in *Intent) updateProfile(ctx context.Context, update func(*Profile) error) error {
	profile, err := in.server.store.getProfile(ctx, in.mxid)
	if err != nil {
		return err
	}
	if err = update(profile); err != nil {
		return err
	}
	return in.server.store.setProfile(ctx, profile)
}

// SetDisplayName sets the global display name of the user. Member events are
// not rewritten: the application reads names from profiles.
func (in *Intent) SetDisplayName(ctx context.Context, name string) error {
	return in.updateProfile(ctx, func(p *Profile) error {
		p.DisplayName = name
		return nil
	})
}

// SetAvatarURL sets the global avatar of the user.
func (in *Intent) SetAvatarURL(ctx context.Context, avatarURL id.ContentURIString) error {
	return in.updateProfile(ctx, func(p *Profile) error {
		p.AvatarURL = avatarURL
		return nil
	})
}

// SetExtraProfileMeta stores the network-specific profile fields.
func (in *Intent) SetExtraProfileMeta(ctx context.Context, data any) error {
	return in.updateProfile(ctx, func(p *Profile) error {
		extra, err := json.Marshal(data)
		p.Extra = extra
		return err
	})
}

// SetProfile replaces the whole profile; the connector does not advertise
// ReplaceEntireProfile, so bridgev2 does not call it, but it is supported.
func (in *Intent) SetProfile(ctx context.Context, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var fields struct {
		DisplayName string              `json:"displayname"`
		AvatarURL   id.ContentURIString `json:"avatar_url"`
	}
	if err = json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	return in.updateProfile(ctx, func(p *Profile) error {
		p.DisplayName, p.AvatarURL, p.Extra = fields.DisplayName, fields.AvatarURL, raw
		return nil
	})
}

// CreateRoom creates a room. Invited users join immediately, as advertised by
// the AutoJoinInvites capability. The room ID is the deterministic one that
// bridgev2 passes, when there is one.
//
// A deterministic room that the same bridge created already is reused: it
// is left behind when the core closes between the creation of a portal's
// room and the saving of the portal, and bridgev2 creates the room again
// with the same ID when the portal comes back. The request's state is then
// applied to the existing room, and the members who left join again.
func (in *Intent) CreateRoom(ctx context.Context, req *mautrix.ReqCreateRoom) (id.RoomID, error) {
	roomID := req.BeeperLocalRoomID
	if roomID == "" {
		roomID = newRoomID(in.server.serverName)
	}
	members := append(append([]id.UserID{}, req.Invite...), req.BeeperInitialMembers...)
	err := in.server.transact(ctx, func(ctx context.Context, store func(*event.Event) error) error {
		existing, err := in.server.store.getRoom(ctx, roomID)
		switch {
		case err == nil && (req.BeeperLocalRoomID == "" || existing.BridgeID != in.conn.bridgeID):
			return fmt.Errorf("room %s already exists", roomID)
		case err != nil && !errors.Is(err, ErrNotFound):
			return err
		}
		reused := err == nil
		emptyKey := ""
		state := func(eventType event.Type, stateKey string, content event.Content) error {
			return store(in.newEvent(roomID, eventType, &stateKey, content, time.Time{}))
		}
		// join adds a member, unless already joined.
		join := func(member id.UserID, isDirect bool) error {
			if reused {
				current, err := in.server.store.getState(ctx, roomID, event.StateMember, string(member))
				if err != nil {
					return err
				}
				if current != nil && current.Content.AsMember().Membership == event.MembershipJoin {
					return nil
				}
			}
			evt, err := in.memberEvent(ctx, roomID, member, event.MembershipJoin, isDirect)
			if err != nil {
				return err
			}
			return store(evt)
		}
		if !reused {
			err = in.server.store.createRoom(ctx, &Room{
				ID: roomID, BridgeID: in.conn.bridgeID, Creator: in.mxid, IsDirect: req.IsDirect, CreatedAt: time.Now(),
			})
			if err != nil {
				return err
			}
			creation := map[string]any{}
			for key, value := range req.CreationContent {
				creation[key] = value
			}
			creation["creator"] = in.mxid
			if err = state(event.StateCreate, emptyKey, event.Content{Raw: creation}); err != nil {
				return err
			}
		}
		if err = join(in.mxid, false); err != nil {
			return err
		}
		powerLevels := req.PowerLevelOverride
		if powerLevels == nil {
			powerLevels = &event.PowerLevelsEventContent{}
		}
		powerLevels.EnsureUserLevel(in.mxid, 100)
		if err = state(event.StatePowerLevels, emptyKey, event.Content{Parsed: powerLevels}); err != nil {
			return err
		}
		if req.Preset == "private_chat" || req.Preset == "trusted_private_chat" {
			err = state(event.StateJoinRules, emptyKey, event.Content{Parsed: &event.JoinRulesEventContent{JoinRule: event.JoinRuleInvite}})
			if err != nil {
				return err
			}
		}
		for _, evt := range req.InitialState {
			stateKey := ""
			if evt.StateKey != nil {
				stateKey = *evt.StateKey
			}
			if err = state(evt.Type, stateKey, evt.Content); err != nil {
				return err
			}
		}
		if req.Name != "" {
			if err = state(event.StateRoomName, emptyKey, event.Content{Parsed: &event.RoomNameEventContent{Name: req.Name}}); err != nil {
				return err
			}
		}
		if req.Topic != "" {
			if err = state(event.StateTopic, emptyKey, event.Content{Parsed: &event.TopicEventContent{Topic: req.Topic}}); err != nil {
				return err
			}
		}
		joined := map[id.UserID]bool{in.mxid: true}
		for _, member := range members {
			if joined[member] {
				continue
			}
			joined[member] = true
			if err = join(member, req.IsDirect); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return roomID, nil
}

// DeleteRoom removes the remote users (ghosts and bot) from a room when
// puppetsOnly is set, so that the user keeps the history; otherwise it
// deletes the room and everything stored with it.
func (in *Intent) DeleteRoom(ctx context.Context, roomID id.RoomID, puppetsOnly bool) error {
	if !puppetsOnly {
		return in.server.store.deleteRoom(ctx, roomID)
	}
	return in.server.transact(ctx, func(ctx context.Context, store func(*event.Event) error) error {
		members, err := in.server.Members(ctx, roomID)
		if err != nil {
			return err
		}
		for userID, member := range members {
			if userID == in.server.userID || member.Membership == event.MembershipLeave {
				continue
			}
			evt, err := in.memberEvent(ctx, roomID, userID, event.MembershipLeave, false)
			if err != nil {
				return err
			}
			if err = store(evt); err != nil {
				return err
			}
		}
		return nil
	})
}

// EnsureJoined makes the user join the room if needed.
func (in *Intent) EnsureJoined(ctx context.Context, roomID id.RoomID, _ ...bridgev2.EnsureJoinedParams) error {
	return in.server.transact(ctx, func(ctx context.Context, store func(*event.Event) error) error {
		return in.ensureJoined(ctx, roomID, store)
	})
}

// EnsureInvited makes another user join the room if needed (invites are
// accepted immediately, see CreateRoom).
func (in *Intent) EnsureInvited(ctx context.Context, roomID id.RoomID, userID id.UserID) error {
	other := &Intent{server: in.server, conn: in.conn, mxid: userID}
	return other.EnsureJoined(ctx, roomID)
}

// TagRoom adds or removes a tag (favourite, low priority, ...) of the user.
func (in *Intent) TagRoom(ctx context.Context, roomID id.RoomID, tag event.RoomTag, isTagged bool) error {
	var tags event.TagEventContent
	if _, err := in.server.store.getRoomAccountData(ctx, in.mxid, roomID, accountDataTags, &tags); err != nil {
		return err
	}
	if tags.Tags == nil {
		tags.Tags = make(event.Tags)
	}
	if isTagged {
		tags.Tags[tag] = event.TagMetadata{}
	} else {
		delete(tags.Tags, tag)
	}
	return in.server.store.setRoomAccountData(ctx, in.mxid, roomID, accountDataTags, &tags)
}

// MuteRoom mutes the room for the user until the given time (zero unmutes).
func (in *Intent) MuteRoom(ctx context.Context, roomID id.RoomID, until time.Time) error {
	var untilMS int64
	if !until.IsZero() {
		untilMS = until.UnixMilli()
	}
	return in.server.store.setRoomAccountData(ctx, in.mxid, roomID, accountDataMutedUntil, map[string]int64{"until": untilMS})
}

// GetEvent returns a stored event.
func (in *Intent) GetEvent(ctx context.Context, roomID id.RoomID, eventID id.EventID) (*event.Event, error) {
	return in.server.store.getEvent(ctx, roomID, eventID)
}
