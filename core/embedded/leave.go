// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"time"

	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/api"
)

// leaveTimeout bounds how long conversations.leave waits for the network.
const leaveTimeout = 30 * time.Second

// conversationsLeave makes the user leave a group on its network, then
// forgets it on this device (API 1.29, docs/ADR/0056). The bridge reports
// the network's answer as the status of the leave event.
func (c *Core) conversationsLeave(ctx context.Context, p api.ConversationsLeaveParams) (api.Empty, error) {
	roomID, err := c.room(ctx, p.ConversationID)
	if err != nil {
		return api.Empty{}, err
	}
	if err = c.allowed(ctx, roomID, "leaving the conversation", func(a *api.ConversationActions) bool { return a.Leave }); err != nil {
		return api.Empty{}, err
	}
	// Subscribed before asking, so that the answer cannot be missed.
	updates, unsubscribe := c.host.Matrix.Subscribe(256)
	defer unsubscribe()
	eventID, err := c.host.Matrix.Leave(ctx, roomID)
	if err != nil {
		return api.Empty{}, err
	}
	timer := time.NewTimer(leaveTimeout)
	defer timer.Stop()
	for {
		select {
		case update, ok := <-updates:
			if !ok {
				return api.Empty{}, newError(api.ErrorCodeNetwork, "the network's answer was missed: try again")
			}
			st := update.MessageStatus
			if st == nil || st.EventID != eventID {
				continue
			}
			switch st.Status {
			case event.MessageStatusSuccess:
				return api.Empty{}, c.forgetLeft(ctx, roomID)
			case event.MessageStatusPending:
				continue
			default:
				reason := st.Message
				if reason == "" {
					reason = "the network refused"
				}
				return api.Empty{}, newError(api.ErrorCodeNetwork, "could not leave the conversation: %s", reason)
			}
		case <-timer.C:
			return api.Empty{}, newError(api.ErrorCodeTimeout, "the network did not answer")
		case <-ctx.Done():
			return api.Empty{}, ctx.Err()
		}
	}
}

// forgetLeft deletes a group the user left: its portal, so that the
// bridge creates a new room if the user joins again, then its room.
func (c *Core) forgetLeft(ctx context.Context, roomID id.RoomID) error {
	room, err := c.host.Matrix.Room(ctx, roomID)
	if err != nil {
		return err
	}
	if br := c.host.Bridge(networkid.BridgeID(room.BridgeID)); br != nil {
		portal, err := br.GetPortalByMXID(ctx, roomID)
		if err != nil {
			return err
		}
		if portal != nil {
			if err = portal.Delete(ctx); err != nil {
				return err
			}
		}
	}
	return c.host.Matrix.ForgetRoom(ctx, roomID)
}
