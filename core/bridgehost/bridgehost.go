// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package bridgehost runs bridgev2 network connectors inside the core
// process, on top of the local Matrix implementation (package localmatrix)
// and one SQLite database: no homeserver and no appservice are involved.
package bridgehost

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/bridgeconfig"
	"maunium.net/go/mautrix/bridgev2/commands"
	"maunium.net/go/mautrix/bridgev2/networkid"

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
	// Sealer, if set, seals the sessions of the logins in the database
	// (sqlite.OpenSealed).
	Sealer sqlite.Sealer
	Log    zerolog.Logger
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
	open := sqlite.Open
	if opts.Sealer != nil {
		open = func(path string) (*dbutil.Database, error) { return sqlite.OpenSealed(path, opts.Sealer) }
	}
	db, err := open(opts.DatabasePath)
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

// commandPrefix is a prefix that no message starts with in practice:
// bridgev2 hands every message starting with the command prefix to the
// command processor instead of the network, and an empty prefix would match
// every message. The application drives logins and settings through the
// core API, never through chat commands: the user also lacks the Commands
// permission, so bridgev2 refuses a command before any handler runs.
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
		// Logging out removes the account's conversations from the device;
		// bridgev2 keeps those another login of the user still reaches.
		// Bad credentials keep them, for the user to log in again.
		CleanupOnLogout: bridgeconfig.CleanupOnLogouts{
			Enabled: true,
			Manual: bridgeconfig.CleanupOnLogout{
				Private:        bridgeconfig.CleanupActionDelete,
				Relayed:        bridgeconfig.CleanupActionDelete,
				SharedNoUsers:  bridgeconfig.CleanupActionDelete,
				SharedHasUsers: bridgeconfig.CleanupActionDelete,
			},
			BadCredentials: bridgeconfig.CleanupOnLogout{
				Private:        bridgeconfig.CleanupActionNothing,
				Relayed:        bridgeconfig.CleanupActionNothing,
				SharedNoUsers:  bridgeconfig.CleanupActionNothing,
				SharedHasUsers: bridgeconfig.CleanupActionNothing,
			},
		},
		Permissions: bridgeconfig.PermissionConfig{
			h.Matrix.UserID().String(): &bridgeconfig.Permissions{
				SendEvents:   true,
				Login:        true,
				DoublePuppet: true,
			},
		},
	}
	log := h.log.With().Str("bridge_id", string(bridgeID)).Logger()
	// bridgev2's own processor: connectors such as mautrix-telegram add
	// their handlers to it in Init and require its concrete type. It never
	// runs a command (see commandPrefix).
	br := bridgev2.NewBridge(bridgeID, h.DB, log, cfg, matrix, network, commands.NewProcessor)
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
	h.Matrix.Stop()
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
	err := h.DB.Close()
	// database/sql's Close does not wait for the connections in use: the
	// handlers that bridgev2 leaves running after Bridge.Stop (mautrix/go#602)
	// may still hold one, or be opening one, and with it the database file,
	// which Windows then refuses to delete or reopen. Wait, a bounded time,
	// until they are all released; their next queries fail once the
	// database is closed.
	if !waitForConnections(h.DB.RawDB, closeWait) {
		h.log.Warn().Dur("waited", closeWait).Msg("Database connections still open after closing it")
	}
	return err
}

// closeWait bounds how long Stop waits for the database's connections.
const closeWait = 5 * time.Second

// waitForConnections waits until db, closed, has no open connection left,
// at most timeout. It reports whether none is left.
func waitForConnections(db *sql.DB, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for db.Stats().OpenConnections > 0 {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
	return true
}
