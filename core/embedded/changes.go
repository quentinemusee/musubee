// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"errors"
	"time"

	"go.mau.fi/util/variationselector"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/localmatrix"
)

// What the user does to messages (API 1.8, docs/ADR/0035): reactions,
// edits, deletions and replies. Each change goes through the local server
// to the conversation's bridge, like a message; the commands wait for the
// network's answer, and undo on this device what the network refused,
// except a deletion.

// actions tells what a portal's network lets the user do to messages
// through a login, or nil when it allows none.
func actions(ctx context.Context, portal *bridgev2.Portal, login *bridgev2.UserLogin) *api.ConversationActions {
	if login == nil || login.Client == nil {
		return nil
	}
	features := login.Client.GetCapabilities(ctx, portal)
	if features == nil {
		return nil
	}
	a := &api.ConversationActions{
		Reply:  features.Reply.Partial(),
		React:  features.Reaction.Partial(),
		Edit:   features.Edit.Partial(),
		Delete: features.Delete.Partial(),
		Media:  mediaActions(features),
		// Since API 1.25, direct conversations only (docs/ADR/0053):
		// networks delete groups for everyone, or let the user leave.
		DeleteChat: features.DeleteChat && portal.RoomType == database.RoomTypeDM,
		// Since API 1.29 (docs/ADR/0056): groups whose connector handles
		// memberships.
		Leave: portal.RoomType != database.RoomTypeDM && handlesMemberships(login),
	}
	if *a == (api.ConversationActions{}) {
		return nil
	}
	return a
}

// handlesMemberships says whether a login's connector takes the user's
// membership changes, leaving included.
func handlesMemberships(login *bridgev2.UserLogin) bool {
	_, ok := login.Client.(bridgev2.MembershipHandlingNetworkAPI)
	return ok
}

// conversationActions returns the actions of a conversation's room, or nil
// while the room is not a portal reached by a login.
func (c *Core) conversationActions(ctx context.Context, roomID id.RoomID) (*api.ConversationActions, error) {
	room, err := c.host.Matrix.Room(ctx, roomID)
	if err != nil {
		return nil, err
	}
	br := c.host.Bridge(networkid.BridgeID(room.BridgeID))
	if br == nil {
		return nil, nil
	}
	portal, err := br.GetPortalByMXID(ctx, roomID)
	if err != nil || portal == nil {
		return nil, err
	}
	_, login, err := portalLogin(ctx, br, portal)
	if err != nil {
		return nil, err
	}
	return actions(ctx, portal, login), nil
}

// target returns a message of a conversation that a change is about: an
// m.room.message event that is not an edit.
func (c *Core) target(ctx context.Context, conversation, message string) (id.RoomID, *event.Event, error) {
	roomID, err := c.room(ctx, conversation)
	if err != nil {
		return "", nil, err
	}
	msgRoom, eventID, err := parseMessageID(message)
	if err != nil {
		return "", nil, err
	}
	notFound := newError(api.ErrorCodeNotFound, "no message %q in conversation %q", message, conversation)
	if msgRoom != roomID {
		return "", nil, notFound
	}
	evt, err := c.host.Matrix.Event(ctx, roomID, eventID)
	if errors.Is(err, localmatrix.ErrNotFound) {
		return "", nil, notFound
	} else if err != nil {
		return "", nil, err
	}
	if evt.Type != event.EventMessage || evt.StateKey != nil || evt.Content.AsMessage().RelatesTo.GetReplaceID() != "" {
		return "", nil, notFound
	}
	return roomID, evt, nil
}

// allowed fails unless the conversation's network allows an action.
func (c *Core) allowed(ctx context.Context, roomID id.RoomID, what string, allows func(*api.ConversationActions) bool) error {
	a, err := c.conversationActions(ctx, roomID)
	if err != nil {
		return err
	}
	if a == nil || !allows(a) {
		return newError(api.ErrorCodeUnsupported, "this conversation's network does not allow %s", what)
	}
	return nil
}

// changeWait is how often change looks for the network's answer.
const changeWait = 50 * time.Millisecond

// change sends one of the user's changes and waits for the network's
// answer. A refused change is retracted on this device when undo is set,
// and fails with network.
func (c *Core) change(ctx context.Context, roomID id.RoomID, eventType event.Type, content any, undo bool) error {
	eventID, err := c.host.Matrix.SendEvent(ctx, roomID, eventType, content)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(changeWait)
	defer ticker.Stop()
	for {
		st, err := c.host.Matrix.MessageStatus(ctx, eventID)
		if err != nil {
			return err
		}
		if st != nil && st.Status != event.MessageStatusPending {
			if st.Status == event.MessageStatusSuccess {
				return nil
			}
			if undo {
				// The answer came: the undo must not depend on the request's
				// time limit.
				if err := c.host.Matrix.Retract(context.WithoutCancel(ctx), roomID, eventID); err != nil {
					return err
				}
			}
			reason := st.Message
			if reason == "" {
				reason = "the network refused the change"
			}
			return newError(api.ErrorCodeNetwork, "%s", reason)
		}
		select {
		case <-ctx.Done():
			// The change is stored and may still go through.
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Core) messagesReact(ctx context.Context, p api.MessagesReactParams) (api.Empty, error) {
	if p.Key == "" {
		return api.Empty{}, newError(api.ErrorCodeInvalidParams, "key is empty")
	}
	// In the form bridgev2 gives the network's reactions, so that the
	// same emoji counts once.
	p.Key = variationselector.Add(p.Key)
	roomID, evt, err := c.target(ctx, p.ConversationID, p.MessageID)
	if err != nil {
		return api.Empty{}, err
	}
	if evt.Unsigned.RedactedBecause != nil {
		return api.Empty{}, newError(api.ErrorCodeInvalidParams, "the message was deleted")
	}
	if err = c.allowed(ctx, roomID, "reactions", func(a *api.ConversationActions) bool { return a.React }); err != nil {
		return api.Empty{}, err
	}
	if existing, err := c.host.Matrix.Reaction(ctx, roomID, evt.ID, p.Key); err != nil || existing != "" {
		return api.Empty{}, err
	}
	content := &event.ReactionEventContent{RelatesTo: event.RelatesTo{Type: event.RelAnnotation, EventID: evt.ID, Key: p.Key}}
	return api.Empty{}, c.change(ctx, roomID, event.EventReaction, content, true)
}

func (c *Core) messagesUnreact(ctx context.Context, p api.MessagesReactParams) (api.Empty, error) {
	if p.Key == "" {
		return api.Empty{}, newError(api.ErrorCodeInvalidParams, "key is empty")
	}
	// In the form bridgev2 gives the network's reactions, so that the
	// same emoji counts once.
	p.Key = variationselector.Add(p.Key)
	roomID, evt, err := c.target(ctx, p.ConversationID, p.MessageID)
	if err != nil {
		return api.Empty{}, err
	}
	reaction, err := c.host.Matrix.Reaction(ctx, roomID, evt.ID, p.Key)
	if err != nil || reaction == "" {
		return api.Empty{}, err
	}
	if err = c.allowed(ctx, roomID, "reactions", func(a *api.ConversationActions) bool { return a.React }); err != nil {
		return api.Empty{}, err
	}
	// Taking a reaction back cannot be undone: a refusal leaves the
	// reaction on the network only.
	return api.Empty{}, c.change(ctx, roomID, event.EventRedaction, &event.RedactionEventContent{Redacts: reaction}, false)
}

func (c *Core) messagesEdit(ctx context.Context, p api.MessagesEditParams) (api.Empty, error) {
	if p.Text == "" {
		return api.Empty{}, newError(api.ErrorCodeInvalidParams, "text is empty")
	}
	roomID, evt, err := c.target(ctx, p.ConversationID, p.MessageID)
	if err != nil {
		return api.Empty{}, err
	}
	content := evt.Content.AsMessage()
	switch {
	case evt.Sender != c.host.Matrix.UserID():
		return api.Empty{}, newError(api.ErrorCodeInvalidParams, "only the user's own messages can be edited")
	case evt.Unsigned.RedactedBecause != nil:
		return api.Empty{}, newError(api.ErrorCodeInvalidParams, "the message was deleted")
	case content.MsgType != event.MsgText && content.MsgType != event.MsgEmote:
		return api.Empty{}, newError(api.ErrorCodeInvalidParams, "only text messages can be edited")
	}
	if err = c.allowed(ctx, roomID, "edits", func(a *api.ConversationActions) bool { return a.Edit }); err != nil {
		return api.Empty{}, err
	}
	edit := &event.MessageEventContent{MsgType: content.MsgType, Body: p.Text}
	edit.SetEdit(evt.ID)
	return api.Empty{}, c.change(ctx, roomID, event.EventMessage, edit, true)
}

func (c *Core) messagesDelete(ctx context.Context, p api.MessagesDeleteParams) (api.Empty, error) {
	roomID, evt, err := c.target(ctx, p.ConversationID, p.MessageID)
	if err != nil {
		return api.Empty{}, err
	}
	switch {
	case evt.Sender != c.host.Matrix.UserID():
		return api.Empty{}, newError(api.ErrorCodeInvalidParams, "only the user's own messages can be deleted")
	case evt.Unsigned.RedactedBecause != nil:
		return api.Empty{}, nil
	}
	// A message still waiting for its account (API 1.14) never reached the
	// network: deleting it cancels it, on this device only (API 1.16).
	if cancelled, err := c.host.Matrix.CancelQueued(ctx, roomID, evt.ID); err != nil || cancelled {
		return api.Empty{}, err
	}
	if err = c.allowed(ctx, roomID, "deletions", func(a *api.ConversationActions) bool { return a.Delete }); err != nil {
		return api.Empty{}, err
	}
	// Erased on this device whatever the network answers: the user asked
	// for the text to go.
	return api.Empty{}, c.change(ctx, roomID, event.EventRedaction, &event.RedactionEventContent{Redacts: evt.ID}, false)
}
