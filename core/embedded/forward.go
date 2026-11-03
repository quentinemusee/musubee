// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"errors"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/localmatrix"
)

// messagesForward sends a copy of a message to another conversation, on
// any network (API 1.26, docs/ADR/0054): its latest text, or its media
// from this device's storage with its caption. The copy is a new message
// of the user's: it answers nothing and says nothing of where it came
// from, as networks have no common way to say it.
func (c *Core) messagesForward(ctx context.Context, p api.MessagesForwardParams) (api.MessagesSendResult, error) {
	sourceRoom, _, err := parseMessageID(p.MessageID)
	if err != nil {
		return api.MessagesSendResult{}, err
	}
	_, original, err := c.target(ctx, conversationID(sourceRoom), p.MessageID)
	if err != nil {
		return api.MessagesSendResult{}, err
	}
	if original.Unsigned.RedactedBecause != nil {
		return api.MessagesSendResult{}, newError(api.ErrorCodeInvalidParams, "the message was deleted")
	}
	// As the user sees it: with its latest edit.
	msg, err := c.messageOf(ctx, original)
	if err != nil {
		return api.MessagesSendResult{}, err
	}
	if msg.Media == nil {
		if msg.Text == "" {
			return api.MessagesSendResult{}, newError(api.ErrorCodeUnsupported, "this message has nothing to forward")
		}
		return c.messagesSend(ctx, api.MessagesSendParams{ConversationID: p.ConversationID, Text: msg.Text})
	}
	return c.forwardMedia(ctx, p.ConversationID, msg)
}

// forwardMedia sends a copy of a media message, read whole from the
// device's storage, with the checks of messages.sendMedia.
func (c *Core) forwardMedia(ctx context.Context, conversation string, msg api.Message) (api.MessagesSendResult, error) {
	media := msg.Media
	if media.Size > maxUploadBytes {
		return api.MessagesSendResult{}, newError(api.ErrorCodeInvalidParams, "media larger than %d bytes cannot be forwarded", maxUploadBytes)
	}
	uri, err := parseMediaID(media.MediaID)
	if err != nil {
		return api.MessagesSendResult{}, err
	}
	part, err := c.host.Matrix.ReadMedia(ctx, uri, 0, maxUploadBytes)
	if errors.Is(err, localmatrix.ErrNotFound) {
		return api.MessagesSendResult{}, newError(api.ErrorCodeNotFound, "the media of this message is not on this device")
	} else if err != nil {
		return api.MessagesSendResult{}, err
	}
	fileName := media.FileName
	if fileName == "" {
		fileName = defaultFileName(msg.Kind)
	}
	// The same path and checks as an upload of the user's
	// (messages.sendMedia), with the media handed in place of an upload.
	return c.sendMedia(ctx, api.MessagesSendMediaParams{
		ConversationID: conversation,
		Kind:           msg.Kind,
		MimeType:       part.MimeType,
		FileName:       fileName,
		Caption:        msg.Text,
		Width:          media.Width,
		Height:         media.Height,
		DurationMS:     media.DurationMS,
	}, mediaSource{data: part.Data, animated: media.Animated})
}

// defaultFileName names media that came without a file name.
func defaultFileName(kind api.MessageKind) string {
	switch kind {
	case api.MessageKindImage:
		return "image"
	case api.MessageKindVideo:
		return "video"
	case api.MessageKindVoice, api.MessageKindAudio:
		return "audio"
	default:
		return "file"
	}
}
