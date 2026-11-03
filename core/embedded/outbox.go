// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"context"
	"sync"

	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Messages sent while their account is not connected wait on the device
// (API 1.14, docs/ADR/0043): the local server holds them, with the status
// queued, and they go when the account connects, in the order they were
// written, even after a restart.

// outbox runs the releases one at a time.
type outbox struct {
	mu      sync.Mutex
	running bool
	again   bool
}

// accountConnected tells whether the account that reaches a room is
// connected. A room whose account the core cannot find yet (a portal being
// made) counts as connected: the message goes, as before API 1.14.
func (c *Core) accountConnected(ctx context.Context, roomID id.RoomID) (bool, error) {
	room, err := c.host.Matrix.Room(ctx, roomID)
	if err != nil {
		return false, err
	}
	br := c.host.Bridge(networkid.BridgeID(room.BridgeID))
	if br == nil {
		return true, nil
	}
	portal, err := br.GetPortalByMXID(ctx, roomID)
	if err != nil || portal == nil {
		return true, err
	}
	receiver, _, err := portalLogin(ctx, br, portal)
	if err != nil || receiver == "" {
		return true, err
	}
	state, err := c.host.Matrix.BridgeState(ctx, room.BridgeID, string(receiver))
	if err != nil || state == nil {
		return true, err
	}
	return state.StateEvent == status.StateConnected, nil
}

// sendOrQueue sends a message of the user's, or holds it while its account
// is not connected.
func (c *Core) sendOrQueue(ctx context.Context, roomID id.RoomID, content *event.MessageEventContent) (id.EventID, error) {
	connected, err := c.accountConnected(ctx, roomID)
	if err != nil {
		return "", err
	}
	if !connected {
		return c.host.Matrix.QueueMessage(ctx, roomID, content)
	}
	// Messages already waiting in the room go first.
	c.releaseQueued(ctx)
	return c.host.Matrix.SendMessage(ctx, roomID, content)
}

// releaseQueuedLater releases the held messages in the background: it is
// called while an update is translated, which must not wait for bridges.
func (c *Core) releaseQueuedLater() {
	c.outbox.mu.Lock()
	if c.outbox.running {
		// The running release looks again when it is done.
		c.outbox.again = true
		c.outbox.mu.Unlock()
		return
	}
	c.outbox.running = true
	c.outbox.mu.Unlock()
	c.forwarding.Add(1)
	go func() {
		defer c.forwarding.Done()
		for {
			c.releaseQueued(c.ctx)
			c.outbox.mu.Lock()
			if !c.outbox.again || c.ctx.Err() != nil {
				c.outbox.running = false
				c.outbox.mu.Unlock()
				return
			}
			c.outbox.again = false
			c.outbox.mu.Unlock()
		}
	}()
}

// releaseQueued hands to their bridges the held messages whose accounts are
// connected, the oldest first. Release takes each message once, however
// many releases run.
func (c *Core) releaseQueued(ctx context.Context) {
	queued, err := c.host.Matrix.Queued(ctx)
	if err != nil {
		c.log.Err(err).Msg("Failed to list the messages waiting for their accounts")
		return
	}
	for _, q := range queued {
		if ctx.Err() != nil {
			return
		}
		connected, err := c.accountConnected(ctx, q.RoomID)
		if err != nil || !connected {
			continue
		}
		if _, err = c.host.Matrix.Release(ctx, q.RoomID, q.EventID); err != nil {
			// No content, nor the room's name, in the log.
			c.log.Err(err).Msg("Failed to send a message that waited for its account")
		}
	}
}
