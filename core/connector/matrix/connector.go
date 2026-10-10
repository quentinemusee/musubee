// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package matrix is the network connector of native Matrix accounts: the
// user's own account on a Matrix homeserver (matrix.org, their company's,
// their own), handled like any other network (docs/ADR/0018). The core is a
// Matrix client of that homeserver, with end-to-end encryption (goolm), and
// mirrors the account's rooms as conversations through bridgev2, like the
// other connectors do with their networks' chats.
package matrix

import (
	"context"
	"fmt"
	"sync"

	"go.mau.fi/util/configupgrade"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/sqlstatestore"
)

// NetworkID is the bridge ID of Matrix accounts in the core.
const NetworkID = "matrix"

// Config configures the connector.
type Config struct {
	// PickleKey encrypts the Olm and Megolm keys in the database. It
	// protects nothing while it is a constant of the code: the secrets at
	// rest are T2.3's (docs/TASKS.md).
	PickleKey []byte
	// DeviceName is the name of the core's device in the user's session
	// list. "Musubee" if empty.
	DeviceName string
}

// Connector is the bridgev2.NetworkConnector of Matrix accounts.
type Connector struct {
	Config Config
	br     *bridgev2.Bridge

	// db holds the Matrix client stores: the room state shared by every
	// account (mx_ tables), and each account's keys and sync position
	// (crypto_ tables, keyed by account).
	db         *dbutil.Database
	stateStore *sqlstatestore.SQLStateStore

	// connecting tracks the connections started in the background after a
	// login, so that Stop returns only once they are done (see the echo
	// connector).
	connectLock sync.Mutex
	connecting  sync.WaitGroup
}

var (
	_ bridgev2.NetworkConnector = (*Connector)(nil)
	_ bridgev2.StoppableNetwork = (*Connector)(nil)
)

// New returns a connector.
func New(cfg Config) *Connector {
	if cfg.DeviceName == "" {
		cfg.DeviceName = "Musubee"
	}
	return &Connector{Config: cfg}
}

// Init is called by bridgev2.NewBridge.
func (c *Connector) Init(br *bridgev2.Bridge) {
	c.br = br
	c.db = br.DB.Database
	c.stateStore = sqlstatestore.NewSQLStateStore(c.db, dbutil.ZeroLogger(br.Log.With().Str("db_section", "matrix_state").Logger()), false)
}

// Start creates or upgrades the Matrix client stores.
func (c *Connector) Start(ctx context.Context) error {
	if len(c.Config.PickleKey) == 0 {
		return fmt.Errorf("the matrix connector needs a pickle key")
	}
	if err := c.stateStore.Upgrade(ctx); err != nil {
		return fmt.Errorf("upgrading the Matrix state store: %w", err)
	}
	// The crypto store's tables are shared by every account: upgrading
	// them through any account's store upgrades them for all.
	if err := c.cryptoStore("", "").DB.Upgrade(ctx); err != nil {
		return fmt.Errorf("upgrading the Matrix crypto store: %w", err)
	}
	return nil
}

func (c *Connector) cryptoStore(account, device string) *crypto.SQLCryptoStore {
	log := dbutil.ZeroLogger(c.br.Log.With().Str("db_section", "matrix_crypto").Logger())
	return crypto.NewSQLCryptoStore(c.db, log, account, idDevice(device), c.Config.PickleKey)
}

// Stop waits for the connections started in the background. bridgev2
// calls it once the logins are disconnected, before the database is closed.
func (c *Connector) Stop() {
	c.connectLock.Lock()
	defer c.connectLock.Unlock()
	c.connecting.Wait()
}

// connectLater connects a new login in the background, unless the bridge is
// stopping.
func (c *Connector) connectLater(login *bridgev2.UserLogin) {
	c.connectLock.Lock()
	defer c.connectLock.Unlock()
	if c.br.IsStopping() {
		return
	}
	c.connecting.Add(1)
	go func() {
		defer c.connecting.Done()
		login.Client.Connect(login.Log.WithContext(c.br.BackgroundCtx))
	}()
}

// GetName describes the network.
func (c *Connector) GetName() bridgev2.BridgeName {
	return bridgev2.BridgeName{
		DisplayName:      "Matrix",
		NetworkURL:       "https://matrix.org",
		NetworkID:        NetworkID,
		BeeperBridgeType: "github.com/quentinemusee/musubee/core/connector/matrix",
	}
}

// GetDBMetaTypes declares the metadata stored with each login.
func (c *Connector) GetDBMetaTypes() database.MetaTypes {
	return database.MetaTypes{
		UserLogin: func() any { return &LoginMetadata{} },
	}
}

// LoginMetadata is what the core keeps of a Matrix session. The access
// token is a secret: it is stored in plaintext until T2.3, like the other
// networks' sessions.
type LoginMetadata struct {
	HomeserverURL string `json:"homeserver_url"`
	AccessToken   string `json:"access_token"`
	DeviceID      string `json:"device_id"`
}

// GetCapabilities returns the general capabilities of the network.
func (c *Connector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return &bridgev2.NetworkGeneralCapabilities{}
}

// GetConfig returns no configuration file: the connector is configured in
// code (Config).
func (c *Connector) GetConfig() (string, any, configupgrade.Upgrader) {
	return "", nil, configupgrade.NoopUpgrader
}

// GetBridgeInfoVersion returns the version of the room metadata the
// connector produces.
func (c *Connector) GetBridgeInfoVersion() (info, capabilities int) {
	return 1, 1
}

// LoadUserLogin attaches a client to a login loaded from the database or
// just created.
func (c *Connector) LoadUserLogin(_ context.Context, login *bridgev2.UserLogin) error {
	meta, ok := login.Metadata.(*LoginMetadata)
	if !ok || meta.AccessToken == "" || meta.HomeserverURL == "" {
		return fmt.Errorf("login %s has no Matrix session", login.ID)
	}
	cl, err := newClient(c, login, meta)
	if err != nil {
		return err
	}
	login.Client = cl
	return nil
}

// keyTables are the crypto store's tables with rows of one device's account
// (mautrix-go v0.31.0, crypto/sql_store_upgrade). The others hold what any
// device may learn about other users' devices, shared by every account.
var keyTables = []string{
	"crypto_olm_message_hash",
	"crypto_olm_session",
	"crypto_megolm_inbound_session",
	"crypto_megolm_outbound_session",
	"crypto_secrets",
	"crypto_account",
}

// deleteKeys deletes the keys of a device that was logged out: they can
// decrypt the account's messages, and no one will use them again.
func (c *Connector) deleteKeys(ctx context.Context, account string) error {
	return c.db.DoTxn(ctx, nil, func(ctx context.Context) error {
		for _, table := range keyTables {
			if _, err := c.db.Exec(ctx, "DELETE FROM "+table+" WHERE account_id=$1", account); err != nil {
				return fmt.Errorf("deleting from %s: %w", table, err)
			}
		}
		return nil
	})
}
