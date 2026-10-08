// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package bridgehost runs bridgev2 network connectors inside the core
// process, on top of the local Matrix implementation (package localmatrix)
// and one SQLite database: no homeserver and no appservice are involved.
package bridgehost

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/bridgeconfig"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/localmatrix"
	"github.com/quentinemusee/musubee/core/storage/sqlite"
)

// Options configures a Host.
type Options struct {
	// DatabasePath is the SQLite file shared by the local Matrix storage and
	// every bridge. It is created if it does not exist.
	DatabasePath string
	// UserLocalpart is the localpart of the device's user ("me" if empty).
	UserLocalpart string
	// PortalWait is localmatrix.Options.PortalWait.
	PortalWait time.Duration
	Log        zerolog.Logger
}

// Host owns the database, the local Matrix server and the bridges.
type Host struct {
	DB     *dbutil.Database
	Matrix *localmatrix.Server
	log    zerolog.Logger

	bridges map[networkid.BridgeID]*bridgev2.Bridge
}

// New opens the database and creates the local Matrix server. Add networks
// with AddNetwork, then call Start.
func New(opts Options) (*Host, error) {
	db, err := sqlite.Open(opts.DatabasePath)
	if err != nil {
		return nil, err
	}
	return &Host{
		DB: db,
		Matrix: localmatrix.New(db, localmatrix.Options{
			UserLocalpart: opts.UserLocalpart,
			PortalWait:    opts.PortalWait,
			Log:           opts.Log.With().Str("component", "localmatrix").Logger(),
		}),
		log:     opts.Log,
		bridges: make(map[networkid.BridgeID]*bridgev2.Bridge),
	}, nil
}

// noCommands is the bridgev2 command processor of in-process bridges: the
// application drives logins and settings through the core API, never through
// chat commands.
type noCommands struct{}

func (noCommands) Handle(context.Context, id.RoomID, id.EventID, *bridgev2.User, string, id.EventID) {
}

// commandPrefix is a prefix that no message starts with in practice:
// bridgev2 hands every message starting with the command prefix to the
// command processor instead of the network, and an empty prefix would match
// every message.
const commandPrefix = "\x00musubee-commands-disabled"

// AddNetwork creates the bridge of one network, identified by bridgeID
// (lowercase letters and digits; it becomes part of local Matrix IDs).
func (h *Host) AddNetwork(bridgeID networkid.BridgeID, network bridgev2.NetworkConnector) (*bridgev2.Bridge, error) {
	if _, exists := h.bridges[bridgeID]; exists {
		return nil, fmt.Errorf("network %q is already added", bridgeID)
	}
	matrix, err := h.Matrix.NewConnector(string(bridgeID))
	if err != nil {
		return nil, err
	}
	cfg := &bridgeconfig.BridgeConfig{
		CommandPrefix: commandPrefix,
		// DM rooms get the name of the contact, so that the application
		// can list conversations without resolving members.
		PrivateChatPortalMeta: true,
		// The upstream default: Matrix events are handled by a per-portal
		// goroutine, so sending returns before the network answers, and
		// the outcome arrives as a message status.
		PortalEventBuffer: 64,
		Permissions: bridgeconfig.PermissionConfig{
			h.Matrix.UserID().String(): &bridgeconfig.Permissions{
				SendEvents:   true,
				Login:        true,
				DoublePuppet: true,
			},
		},
	}
	log := h.log.With().Str("bridge_id", string(bridgeID)).Logger()
	br := bridgev2.NewBridge(bridgeID, h.DB, log, cfg, matrix, network, func(*bridgev2.Bridge) bridgev2.CommandProcessor {
		return noCommands{}
	})
	// Every bridge shares the host's database: stopping one network must
	// not close it under the others (bridgev2 closes it otherwise).
	br.ExternallyManagedDB = true
	h.bridges[bridgeID] = br
	return br, nil
}

// Bridge returns the bridge of a network, or nil.
func (h *Host) Bridge(bridgeID networkid.BridgeID) *bridgev2.Bridge {
	return h.bridges[bridgeID]
}

// Start starts every bridge: it creates or upgrades the tables and
// reconnects the saved logins.
func (h *Host) Start(ctx context.Context) error {
	for bridgeID, br := range h.bridges {
		// bridgev2 leaves the upgrade of an externally managed database to
		// its owner. The tables are shared (rows carry the bridge ID), so
		// the first bridge creates them and the others find them current.
		if err := br.DB.Upgrade(ctx); err != nil {
			return fmt.Errorf("upgrading the tables of bridge %q: %w", bridgeID, err)
		}
		if err := br.Start(ctx); err != nil {
			return fmt.Errorf("starting bridge %q: %w", bridgeID, err)
		}
	}
	return nil
}

// User returns the bridgev2 user of the device's user, used to start logins.
func (h *Host) User(ctx context.Context, bridgeID networkid.BridgeID) (*bridgev2.User, error) {
	br := h.bridges[bridgeID]
	if br == nil {
		return nil, fmt.Errorf("unknown network %q", bridgeID)
	}
	return br.GetUserByMXID(ctx, h.Matrix.UserID())
}

// Stop stops every bridge and closes the database. A stopped host cannot be
// started again: create a new one.
func (h *Host) Stop() error {
	for _, br := range h.bridges {
		br.Stop()
		// bridgev2 only destroys the bridge state queue of a login when the
		// login is deleted: in a long-running bridge process, stopping means
		// exiting. In the app, the core stops and starts within one process
		// (closing the window, mobile background), and each queue would leak
		// a goroutine holding the whole bridge. Same steps as
		// UserLogin.Delete; Send accepts a nil queue.
		for _, login := range br.GetAllCachedUserLogins() {
			if login.BridgeState != nil {
				login.BridgeState.Destroy()
				login.BridgeState = nil
			}
		}
	}
	return errors.Join(h.DB.Close())
}
