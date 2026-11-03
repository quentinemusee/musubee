// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package matrix

import (
	"context"
	"errors"
	"slices"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// A Matrix room's older history (docs/ADR/0045): bridgev2 asks for it when
// the user scrolls past the oldest message on the device (ADR 0041); the
// connector pages backwards through the homeserver's timeline (/messages),
// decrypting with the keys the device has, which a recovery key may have
// restored (ADR 0044).

var _ bridgev2.BackfillingNetworkAPI = (*client)(nil)

// historyLimit is the most events asked of the homeserver at once.
const historyLimit = 100

// FetchMessages gives a page of a room's history before the oldest message
// the bridge has. Forward fetches give nothing: the sync brings what
// happens after the login.
func (c *client) FetchMessages(ctx context.Context, params bridgev2.FetchMessagesParams) (*bridgev2.FetchMessagesResponse, error) {
	if params.Forward {
		return &bridgev2.FetchMessagesResponse{Forward: true}, nil
	}
	if err := c.isReady(); err != nil {
		return nil, err
	}
	room := id.RoomID(params.Portal.ID)
	from := string(params.Cursor)
	if from == "" && params.AnchorMessage != nil {
		// The token just before the oldest bridged message.
		around, err := c.cli.Context(ctx, room, id.EventID(params.AnchorMessage.ID), nil, 0)
		if err != nil {
			return nil, err
		}
		from = around.Start
	}
	count := min(max(params.Count, 1), historyLimit)
	resp, err := c.cli.Messages(ctx, room, from, "", mautrix.DirectionBackward, &mautrix.FilterPart{Types: []event.Type{event.EventMessage, event.EventEncrypted}}, count)
	if err != nil {
		return nil, err
	}
	messages := make([]*bridgev2.BackfillMessage, 0, len(resp.Chunk))
	for _, evt := range resp.Chunk {
		if msg := c.historyMessage(ctx, params.Portal, room, evt); msg != nil {
			messages = append(messages, msg)
		}
	}
	// The homeserver gives the newest first; bridgev2 wants the oldest.
	slices.Reverse(messages)
	return &bridgev2.FetchMessagesResponse{
		Messages: messages,
		Cursor:   networkid.PaginationCursor(resp.End),
		HasMore:  resp.End != "" && len(resp.Chunk) > 0,
	}, nil
}

// historyMessage converts one event of the history, or nil for what the
// thread does not show (edits, redacted or empty events): edits of older
// messages are not applied (a later task), the message shows as sent.
func (c *client) historyMessage(ctx context.Context, portal *bridgev2.Portal, room id.RoomID, evt *event.Event) *bridgev2.BackfillMessage {
	evt.RoomID = room
	if err := evt.Content.ParseRaw(evt.Type); err != nil && !errors.Is(err, event.ErrContentAlreadyParsed) {
		return nil
	}
	var content *event.MessageEventContent
	if evt.Type == event.EventEncrypted {
		decrypted, err := c.crypto.Decrypt(ctx, evt)
		if err != nil {
			// No key on the device (or in the restored backup).
			content = &event.MessageEventContent{MsgType: event.MsgNotice, Body: undecryptable}
		} else if decrypted.Type == event.EventMessage {
			content = decrypted.Content.AsMessage()
		} else {
			return nil
		}
	} else {
		content = evt.Content.AsMessage()
	}
	if evt.Unsigned.RedactedBecause != nil || content.Body == "" && content.MsgType == "" {
		return nil
	}
	if content.RelatesTo.GetReplaceID() != "" {
		return nil
	}
	converted, replyTo := incoming(content)
	c.copyMedia(ctx, portal, c.connector.br.Bot, converted)
	c.senderGhost(ctx, room, evt.Sender)
	return &bridgev2.BackfillMessage{
		ConvertedMessage: &bridgev2.ConvertedMessage{
			ReplyTo: replyTo,
			Parts:   []*bridgev2.ConvertedMessagePart{{Type: event.EventMessage, Content: converted}},
		},
		Sender:    bridgev2.EventSender{Sender: networkid.UserID(evt.Sender), IsFromMe: evt.Sender == c.self},
		ID:        networkid.MessageID(evt.ID),
		Timestamp: time.UnixMilli(evt.Timestamp),
	}
}
