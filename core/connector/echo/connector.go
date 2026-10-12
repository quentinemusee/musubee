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
// and one group conversation, GroupEcho, whose members are named like
// ContactInstant and ContactDelayed, and where the first one echoes every
// message back. They are other remote users than the contacts: in
// mautrix-go v0.31.0, two portals that update the same ghost at once race
// (Ghost.UpdateInfo writes its fields without a lock), and the direct and
// group conversations of a login are created at once.
//
// The username selects behaviours for the tests of user interfaces:
//
//   - a username starting with CrowdPrefix also has CrowdSize contacts who
//     each sent one message, a minute apart, the last one most recently;
//   - a username starting with OfflinePrefix connects right after the login,
//     but never again: from the next start of the core on, it stays
//     disconnected, as on a device without network;
//   - a username starting with ExpiredPrefix connects right after the login,
//     but from the next start of the core on, its session is refused, as when
//     a network ends a session: the user must log in again.
//
// It replaces Beeper's dummybridge, which has no license (docs/ADR/0004).
package echo

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
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

// The members of GroupEcho, named like ContactInstant and ContactDelayed
// (see the package documentation for why they are not the same users).
const (
	MemberInstant networkid.UserID = "echo-group-instant"
	MemberDelayed networkid.UserID = "echo-group-delayed"
)

var groupMembers = []Contact{
	{ID: MemberInstant, Name: "Instant Echo"},
	{ID: MemberDelayed, Name: "Delayed Echo"},
}

func findContact(userID networkid.UserID) (Contact, bool) {
	for _, contact := range slices.Concat(Contacts, groupMembers) {
		if contact.ID == userID {
			return contact, true
		}
	}
	return crowdContact(userID)
}

// GroupEcho is the portal ID of the group conversation of every login.
const GroupEcho networkid.PortalID = "group-echo"

// Group is a fake group conversation.
type Group struct {
	ID      networkid.PortalID
	Name    string
	Members []networkid.UserID
}

// Groups lists the group conversations of every login.
var Groups = []Group{
	{ID: GroupEcho, Name: "Echo Group", Members: []networkid.UserID{MemberInstant, MemberDelayed}},
}

func findGroup(portalID networkid.PortalID) (Group, bool) {
	for _, group := range Groups {
		if group.ID == portalID {
			return group, true
		}
	}
	return Group{}, false
}

// Behaviours selected by the username (see the package documentation).
const (
	CrowdPrefix   = "crowd"
	CrowdSize     = 120
	OfflinePrefix = "offline"
	ExpiredPrefix = "expired"

	crowdIDPrefix = "echo-crowd-"
)

// crowdContact returns the contact of a crowd contact's ID: a contact who
// echoes like ContactInstant, named after their number.
func crowdContact(userID networkid.UserID) (Contact, bool) {
	number, ok := strings.CutPrefix(string(userID), crowdIDPrefix)
	if n, err := strconv.Atoi(number); !ok || err != nil || n < 1 || n > CrowdSize || number != fmt.Sprintf("%03d", n) {
		return Contact{}, false
	}
	return Contact{ID: userID, Name: "Crowd Contact " + number}, true
}

// contactsOf returns the contacts of a login: Contacts, then the crowd
// for a username starting with CrowdPrefix.
func contactsOf(login *bridgev2.UserLogin) []Contact {
	if !strings.HasPrefix(login.RemoteName, CrowdPrefix) {
		return Contacts
	}
	contacts := slices.Clone(Contacts)
	for n := 1; n <= CrowdSize; n++ {
		contact, _ := crowdContact(networkid.UserID(fmt.Sprintf("%s%03d", crowdIDPrefix, n)))
		contacts = append(contacts, contact)
	}
	return contacts
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

	// connecting tracks the connections started in the background after a
	// login, so that Stop returns only once they are done: until then, they
	// use the login's bridge state and the database.
	connectLock sync.Mutex
	connecting  sync.WaitGroup
}

var (
	_ bridgev2.NetworkConnector = (*Connector)(nil)
	_ bridgev2.StoppableNetwork = (*Connector)(nil)
)

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

// Stop waits for the connections started in the background. bridgev2
// calls it once the logins are disconnected, before the database is closed.
func (c *Connector) Stop() {
	// The bridge is already stopping: connectLater starts nothing once it
	// gets the lock, and the connections do not take it.
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
	FlowQR           = "qr"
	stepUsername     = "com.musubee.echo.username"
	stepCode         = "com.musubee.echo.code"
	stepQR           = "com.musubee.echo.qr"
	qrPrefix         = "musubee-echo:"
	fieldUsername    = "username"
	usernamePattern  = `^[a-z0-9]{1,32}$`
	stepCompleteID   = "com.musubee.echo.complete"
	selfIDPrefix     = "user-"
	loginInstruction = "Choose any username: the echo network has no accounts."
)

// GetLoginFlows lists the login flows: choosing a username, or a code or a
// QR code confirmed by the network itself (stand-ins for the QR code logins
// that a phone confirms).
func (c *Connector) GetLoginFlows() []bridgev2.LoginFlow {
	return []bridgev2.LoginFlow{{
		Name:        "Username",
		Description: "Log in with any username.",
		ID:          FlowUsername,
	}, {
		Name:        "Code",
		Description: "Show a code that the network confirms by itself after the echo delay.",
		ID:          FlowCode,
	}, {
		Name:        "QR code",
		Description: "Show a QR code that the network confirms by itself after the echo delay.",
		ID:          FlowQR,
	}}
}

// CreateLogin starts a login.
func (c *Connector) CreateLogin(_ context.Context, user *bridgev2.User, flowID string) (bridgev2.LoginProcess, error) {
	switch flowID {
	case FlowUsername:
		return &loginProcess{connector: c, user: user}, nil
	case FlowCode:
		return newCodeLoginProcess(c, user, bridgev2.LoginDisplayTypeCode), nil
	case FlowQR:
		return newCodeLoginProcess(c, user, bridgev2.LoginDisplayTypeQR), nil
	default:
		return nil, fmt.Errorf("unknown login flow %q", flowID)
	}
}
