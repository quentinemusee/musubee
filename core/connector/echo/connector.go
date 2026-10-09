// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package echo is a fake network, written with mautrix-go's bridgev2
// framework, for tests that need a network connector without a real network.
//
// Logging in only asks for a username. Every login has the same three
// contacts, each with a direct conversation:
//
//   - ContactInstant echoes every message back immediately;
//   - ContactDelayed echoes every message back after Config.EchoDelay;
//   - ContactUnreachable rejects every message: sending fails.
//
// It replaces Beeper's dummybridge, which has no license (docs/ADR/0004).
package echo

import (
	"context"
	"fmt"
	"time"

	"go.mau.fi/util/configupgrade"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

// The remote user IDs of the contacts.
const (
	ContactInstant     networkid.UserID = "echo-instant"
	ContactDelayed     networkid.UserID = "echo-delayed"
	ContactUnreachable networkid.UserID = "echo-unreachable"
)

// Contact is a fake remote user.
type Contact struct {
	ID   networkid.UserID
	Name string
}

// Contacts lists the contacts of every login, in a stable order.
var Contacts = []Contact{
	{ID: ContactInstant, Name: "Instant Echo"},
	{ID: ContactDelayed, Name: "Delayed Echo"},
	{ID: ContactUnreachable, Name: "Unreachable Contact"},
}

func findContact(userID networkid.UserID) (Contact, bool) {
	for _, contact := range Contacts {
		if contact.ID == userID {
			return contact, true
		}
	}
	return Contact{}, false
}

// DefaultEchoDelay is the delay of ContactDelayed when Config.EchoDelay is zero.
const DefaultEchoDelay = 2 * time.Second

// Config configures the fake network.
type Config struct {
	// EchoDelay is how long ContactDelayed waits before echoing a message.
	EchoDelay time.Duration
}

// Connector is the bridgev2.NetworkConnector of the fake network.
type Connector struct {
	Config Config
	br     *bridgev2.Bridge
}

var _ bridgev2.NetworkConnector = (*Connector)(nil)

// New returns a connector for the fake network.
func New(cfg Config) *Connector {
	if cfg.EchoDelay <= 0 {
		cfg.EchoDelay = DefaultEchoDelay
	}
	return &Connector{Config: cfg}
}

// Init is called by bridgev2.NewBridge.
func (c *Connector) Init(br *bridgev2.Bridge) {
	c.br = br
}

// Start does nothing: there is no network to connect to.
func (c *Connector) Start(context.Context) error {
	return nil
}

// GetName describes the fake network.
func (c *Connector) GetName() bridgev2.BridgeName {
	return bridgev2.BridgeName{
		DisplayName:      "Echo",
		NetworkID:        "echo",
		BeeperBridgeType: "github.com/quentinemusee/musubee/core/connector/echo",
	}
}

// GetDBMetaTypes returns no metadata types: the fake network stores nothing
// beyond what bridgev2 stores.
func (c *Connector) GetDBMetaTypes() database.MetaTypes {
	return database.MetaTypes{}
}

// GetCapabilities returns the general capabilities of the fake network.
func (c *Connector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return &bridgev2.NetworkGeneralCapabilities{}
}

// GetConfig returns no configuration file: the fake network is configured in
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
	login.Client = newClient(c, login)
	return nil
}

// Login flows.
const (
	FlowUsername     = "username"
	FlowCode         = "code"
	stepUsername     = "com.musubee.echo.username"
	stepCode         = "com.musubee.echo.code"
	fieldUsername    = "username"
	usernamePattern  = `^[a-z0-9]{1,32}$`
	stepCompleteID   = "com.musubee.echo.complete"
	selfIDPrefix     = "user-"
	loginInstruction = "Choose any username: the echo network has no accounts."
)

// GetLoginFlows lists the login flows: choosing a username, or a code
// confirmed by the network itself (a stand-in for QR code logins).
func (c *Connector) GetLoginFlows() []bridgev2.LoginFlow {
	return []bridgev2.LoginFlow{{
		Name:        "Username",
		Description: "Log in with any username.",
		ID:          FlowUsername,
	}, {
		Name:        "Code",
		Description: "Show a code that the network confirms by itself after the echo delay.",
		ID:          FlowCode,
	}}
}

// CreateLogin starts a login.
func (c *Connector) CreateLogin(_ context.Context, user *bridgev2.User, flowID string) (bridgev2.LoginProcess, error) {
	switch flowID {
	case FlowUsername:
		return &loginProcess{connector: c, user: user}, nil
	case FlowCode:
		return newCodeLoginProcess(c, user), nil
	default:
		return nil, fmt.Errorf("unknown login flow %q", flowID)
	}
}
