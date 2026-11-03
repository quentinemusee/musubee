// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"slices"
	"strings"
	"sync"
	"time"

	"maunium.net/go/mautrix/event"

	"github.com/quentinemusee/musubee/core/api"
)

// This file receives the media the user sends (API 1.10, docs/ADR/0037):
// the interface gives its bytes in parts (uploads.append), then sends it
// (messages.sendMedia).

const (
	// maxUploadBytes is the largest media the user can send: 100 MiB.
	maxUploadBytes = 100 << 20
	// maxUploadsBytes bounds what all uploads hold in memory together.
	maxUploadsBytes = 2 * maxUploadBytes
	// uploadIdle is how long an upload nobody adds to is kept.
	uploadIdle   = 10 * time.Minute
	uploadPrefix = "u."
)

type upload struct {
	data []byte
	used time.Time
}

// uploads holds the uploads in progress, in memory only: media the user
// sends never touches the disk before it is stored with its message.
type uploads struct {
	mu   sync.Mutex
	byID map[string]*upload
	// Limits for the tests; 0 is maxUploadBytes and maxUploadsBytes.
	maxBytes, maxTotal int
	nowFunc            func() time.Time
}

func (u *uploads) now() time.Time {
	if u.nowFunc != nil {
		return u.nowFunc()
	}
	return time.Now()
}

// dropIdleLocked forgets the uploads left alone for uploadIdle, and returns
// what the others hold.
func (u *uploads) dropIdleLocked() int {
	total := 0
	for uploadID, up := range u.byID {
		if u.now().Sub(up.used) > uploadIdle {
			delete(u.byID, uploadID)
			continue
		}
		total += len(up.data)
	}
	return total
}

func (u *uploads) append(uploadID string, data []byte) (string, int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	total := u.dropIdleLocked()
	if u.byID == nil {
		u.byID = map[string]*upload{}
	}
	up := u.byID[uploadID]
	if uploadID == "" {
		b := make([]byte, 12)
		_, _ = rand.Read(b)
		uploadID, up = uploadPrefix+hex.EncodeToString(b), &upload{}
		u.byID[uploadID] = up
	} else if up == nil {
		return "", 0, newError(api.ErrorCodeNotFound, "no upload %q", uploadID)
	}
	maxBytes, maxTotal := cmp.Or(u.maxBytes, maxUploadBytes), cmp.Or(u.maxTotal, maxUploadsBytes)
	if len(up.data)+len(data) > maxBytes {
		delete(u.byID, uploadID)
		return "", 0, newError(api.ErrorCodeInvalidParams, "a media can be at most %d bytes", maxBytes)
	}
	if total+len(data) > maxTotal {
		return "", 0, newError(api.ErrorCodeInvalidParams, "too many uploads in progress: send or cancel some first")
	}
	up.data = append(up.data, data...)
	up.used = u.now()
	return uploadID, len(up.data), nil
}

// take removes an upload and returns its bytes.
func (u *uploads) take(uploadID string) ([]byte, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.dropIdleLocked()
	up := u.byID[uploadID]
	if up == nil || len(up.data) == 0 {
		return nil, newError(api.ErrorCodeNotFound, "no upload %q", uploadID)
	}
	delete(u.byID, uploadID)
	return up.data, nil
}

// size returns what an upload holds, 0 for an unknown one.
func (u *uploads) size(uploadID string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	if up := u.byID[uploadID]; up != nil {
		return len(up.data)
	}
	return 0
}

func (u *uploads) cancel(uploadID string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.byID, uploadID)
}

func (c *Core) uploadsAppend(_ context.Context, p api.UploadsAppendParams) (api.UploadsAppendResult, error) {
	data, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil || len(data) > maxMediaRead {
		return api.UploadsAppendResult{}, newError(api.ErrorCodeInvalidParams, "data must be base64 of at most %d bytes", maxMediaRead)
	}
	uploadID, size, err := c.uploads.append(p.UploadID, data)
	return api.UploadsAppendResult{UploadID: uploadID, Size: int64(size)}, err
}

func (c *Core) uploadsCancel(_ context.Context, p api.UploadsCancelParams) (api.Empty, error) {
	c.uploads.cancel(p.UploadID)
	return api.Empty{}, nil
}

// mediaMsgTypes are the Matrix message types of the kinds of media; voice
// is an m.audio marked as voice (MSC3245).
var mediaMsgTypes = map[api.MessageKind]event.MessageType{
	api.MessageKindImage: event.MsgImage,
	api.MessageKindVideo: event.MsgVideo,
	api.MessageKindAudio: event.MsgAudio,
	api.MessageKindVoice: event.MsgAudio,
	api.MessageKindFile:  event.MsgFile,
}

// mediaActions returns the kinds of media a network takes, from its room
// features: a kind is taken when bridgev2 would let it through
// (Portal.checkMessageContentCaps looks up File by GetCapMsgType).
func mediaActions(features *event.RoomFeatures) *api.MediaActions {
	of := func(capType event.CapabilityMsgType) *api.MediaAction {
		feat, ok := features.File[capType]
		if !ok || feat == nil {
			return nil
		}
		types, ok := mimeTypes(feat)
		if !ok {
			return nil
		}
		return &api.MediaAction{MaxSize: feat.MaxSize, Caption: feat.Caption.Partial(), MimeTypes: types}
	}
	a := &api.MediaActions{
		Image: of(event.MsgImage),
		Video: of(event.MsgVideo),
		Audio: of(event.MsgAudio),
		Voice: of(event.CapMsgVoice),
		File:  of(event.MsgFile),
	}
	if *a == (api.MediaActions{}) {
		return nil
	}
	return a
}

// mimeTypes lists the media types a network takes for a kind (at least
// partly: bridgev2 refuses only the rejected ones), nil for any; ok is
// false when it takes none, so that the kind is not offered.
func mimeTypes(feat *event.FileFeatures) (types []string, ok bool) {
	for mimeType, level := range feat.MimeTypes {
		if !level.Partial() {
			continue
		}
		if mimeType == "*/*" {
			return nil, true
		}
		types = append(types, mimeType)
	}
	slices.Sort(types)
	return types, len(types) > 0
}

// mediaAction returns what a conversation's actions say of a kind.
func mediaAction(a *api.ConversationActions, kind api.MessageKind) *api.MediaAction {
	if a == nil || a.Media == nil {
		return nil
	}
	switch kind {
	case api.MessageKindImage:
		return a.Media.Image
	case api.MessageKindVideo:
		return a.Media.Video
	case api.MessageKindAudio:
		return a.Media.Audio
	case api.MessageKindVoice:
		return a.Media.Voice
	case api.MessageKindFile:
		return a.Media.File
	}
	return nil
}

func (c *Core) messagesSendMedia(ctx context.Context, p api.MessagesSendMediaParams) (api.MessagesSendResult, error) {
	return c.sendMedia(ctx, p, mediaSource{})
}

// mediaSource is where sendMedia takes the media from: the upload that
// p.UploadID names, or bytes the core already holds (a forwarded message,
// docs/ADR/0054).
type mediaSource struct {
	data []byte
	// animated keeps a GIF a GIF where the network shows one.
	animated bool
}

func (c *Core) sendMedia(ctx context.Context, p api.MessagesSendMediaParams, src mediaSource) (api.MessagesSendResult, error) {
	msgType, ok := mediaMsgTypes[p.Kind]
	if !ok {
		return api.MessagesSendResult{}, newError(api.ErrorCodeInvalidParams, "kind must be image, video, audio, voice or file")
	}
	if strings.TrimSpace(p.FileName) == "" || strings.ContainsAny(p.FileName, "/\\\x00") {
		return api.MessagesSendResult{}, newError(api.ErrorCodeInvalidParams, "file_name must be a plain file name")
	}
	roomID, err := c.room(ctx, p.ConversationID)
	if err != nil {
		return api.MessagesSendResult{}, err
	}
	actions, err := c.conversationActions(ctx, roomID)
	if err != nil {
		return api.MessagesSendResult{}, err
	}
	allowed := mediaAction(actions, p.Kind)
	if allowed == nil {
		return api.MessagesSendResult{}, newError(api.ErrorCodeUnsupported, "this conversation's network does not take %s messages", p.Kind)
	}
	if p.Caption != "" && !allowed.Caption {
		return api.MessagesSendResult{}, newError(api.ErrorCodeUnsupported, "this conversation's network takes no caption with %s messages", p.Kind)
	}
	content := &event.MessageEventContent{
		MsgType:  msgType,
		Body:     p.FileName,
		FileName: p.FileName,
		Info: &event.FileInfo{
			MimeType: p.MimeType,
			Width:    int(p.Width),
			Height:   int(p.Height),
			Duration: int(p.DurationMS),
		},
	}
	if p.Caption != "" {
		content.Body = p.Caption
	}
	if p.Kind == api.MessageKindVoice {
		content.MSC3245Voice = &event.MSC3245Voice{}
		content.MSC1767Audio = &event.MSC1767Audio{Duration: int(p.DurationMS)}
	}
	if p.ReplyTo != "" {
		_, target, err := c.target(ctx, p.ConversationID, p.ReplyTo)
		if err != nil {
			return api.MessagesSendResult{}, err
		}
		if target.Unsigned.RedactedBecause != nil {
			return api.MessagesSendResult{}, newError(api.ErrorCodeInvalidParams, "cannot answer message %q", p.ReplyTo)
		}
		if actions == nil || !actions.Reply {
			return api.MessagesSendResult{}, newError(api.ErrorCodeUnsupported, "this conversation's network does not allow replies")
		}
		content.RelatesTo = (&event.RelatesTo{}).SetReplyTo(target.ID)
	}
	// Checked last: a refused command keeps the upload, to try again.
	size := c.uploads.size(p.UploadID)
	if src.data != nil {
		size = len(src.data)
	}
	if allowed.MaxSize > 0 && int64(size) > allowed.MaxSize {
		return api.MessagesSendResult{}, newError(api.ErrorCodeInvalidParams, "this conversation's network takes %s messages of at most %d bytes", p.Kind, allowed.MaxSize)
	}
	data := src.data
	if data == nil {
		if data, err = c.uploads.take(p.UploadID); err != nil {
			return api.MessagesSendResult{}, err
		}
	}
	content.Info.Size = len(data)
	content.Info.MauGIF = src.animated
	if content.URL, err = c.host.Matrix.UploadMedia(ctx, roomID, data, p.FileName, p.MimeType); err != nil {
		return api.MessagesSendResult{}, err
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
