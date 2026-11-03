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
	resp, err := c.cli.Messages(ctx, room, from, "", mautrix.DirectionBackward,
		&mautrix.FilterPart{Types: []event.Type{event.EventMessage, event.EventEncrypted, event.EventReaction}}, count)
	if err != nil {
		return nil, err
	}
	// Decrypted first: edits and reactions may be encrypted too.
	page := make([]*event.Event, 0, len(resp.Chunk))
	for _, evt := range resp.Chunk {
		if clear := c.historyEvent(ctx, room, evt); clear != nil {
			page = append(page, clear)
		}
	}
	edits, reactions := historyChanges(page)
	messages := make([]*bridgev2.BackfillMessage, 0, len(page))
	for _, evt := range page {
		msg := c.historyMessage(ctx, params.Portal, room, evt, edits[evt.ID])
		if msg == nil {
			continue
		}
		for _, r := range reactions[evt.ID] {
			c.senderGhost(ctx, room, r.Sender)
			key := r.Content.AsReaction().RelatesTo.Key
			msg.Reactions = append(msg.Reactions, &bridgev2.BackfillReaction{
				Timestamp: time.UnixMilli(r.Timestamp),
				Sender:    bridgev2.EventSender{Sender: networkid.UserID(r.Sender), IsFromMe: r.Sender == c.self},
				// The reaction's event ID, as for live reactions (onReaction).
				EmojiID: networkid.EmojiID(r.ID),
				Emoji:   key,
			})
		}
		messages = append(messages, msg)
	}
	// The homeserver gives the newest first; bridgev2 wants the oldest.
	slices.Reverse(messages)
	return &bridgev2.FetchMessagesResponse{
		Messages: messages,
		Cursor:   networkid.PaginationCursor(resp.End),
		HasMore:  resp.End != "" && len(resp.Chunk) > 0,
	}, nil
}

// historyEvent returns an event of the history in clear: decrypted when it
// was encrypted, an "undecryptable" notice when the device has no key for
// it, or nil for what the history does not show (redacted events).
func (c *client) historyEvent(ctx context.Context, room id.RoomID, evt *event.Event) *event.Event {
	evt.RoomID = room
	if evt.Unsigned.RedactedBecause != nil {
		return nil
	}
	if err := evt.Content.ParseRaw(evt.Type); err != nil && !errors.Is(err, event.ErrContentAlreadyParsed) {
		return nil
	}
	if evt.Type != event.EventEncrypted {
		return evt
	}
	decrypted, err := c.crypto.Decrypt(ctx, evt)
	if err != nil {
		notice := *evt
		notice.Type = event.EventMessage
		notice.Content = event.Content{Parsed: &event.MessageEventContent{MsgType: event.MsgNotice, Body: undecryptable}}
		return &notice
	}
	return decrypted
}

// historyChanges finds, in a page of history, the latest edit of each
// message by its sender, and the reactions to each message. Changes whose
// message is on another page are not applied.
func historyChanges(page []*event.Event) (map[id.EventID]*event.MessageEventContent, map[id.EventID][]*event.Event) {
	senders := map[id.EventID]id.UserID{}
	for _, evt := range page {
		senders[evt.ID] = evt.Sender
	}
	edits := map[id.EventID]*event.MessageEventContent{}
	editTimes := map[id.EventID]int64{}
	reactions := map[id.EventID][]*event.Event{}
	for _, evt := range page {
		switch evt.Type {
		case event.EventMessage:
			content := evt.Content.AsMessage()
			target := content.RelatesTo.GetReplaceID()
			if target == "" || content.NewContent == nil || senders[target] != evt.Sender || evt.Timestamp < editTimes[target] {
				continue
			}
			edits[target], editTimes[target] = content.NewContent, evt.Timestamp
		case event.EventReaction:
			rel := evt.Content.AsReaction().RelatesTo
			if rel.Type == event.RelAnnotation && rel.Key != "" {
				reactions[rel.EventID] = append(reactions[rel.EventID], evt)
			}
		}
	}
	return edits, reactions
}

// historyMessage converts a message of the history, as its latest edit
// in the page left it (edited, when not nil), or nil for what the thread
// does not show (edits, reactions, empty events).
func (c *client) historyMessage(ctx context.Context, portal *bridgev2.Portal, room id.RoomID, evt *event.Event, edited *event.MessageEventContent) *bridgev2.BackfillMessage {
	if evt.Type != event.EventMessage {
		return nil
	}
	content := evt.Content.AsMessage()
	if content.RelatesTo.GetReplaceID() != "" || content.Body == "" && content.MsgType == "" {
		return nil
	}
	converted, replyTo := incoming(content)
	if edited != nil {
		// The latest text; the reply stays the original's.
		converted, _ = incoming(edited)
	}
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
