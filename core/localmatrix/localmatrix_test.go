// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package localmatrix_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/localmatrix"
	"github.com/quentinemusee/musubee/core/storage/sqlite"
)

// newServer returns a server on a fresh SQLite file, with its tables created.
func newServer(t *testing.T, opts localmatrix.Options) *localmatrix.Server {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	opts.Log = zerolog.Nop()
	server := localmatrix.New(db, opts)
	if err = server.Upgrade(t.Context()); err != nil {
		t.Fatal(err)
	}
	return server
}

func newConnector(t *testing.T, server *localmatrix.Server, bridgeID string) *localmatrix.Connector {
	t.Helper()
	conn, err := server.NewConnector(bridgeID)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestNewConnectorValidatesBridgeID(t *testing.T) {
	server := newServer(t, localmatrix.Options{UserLocalpart: "me"})
	newConnector(t, server, "echo")
	for _, bridgeID := range []string{"", "Echo", "e_x", "e-x", "e:x", strings.Repeat("a", 33), "echo"} {
		if _, err := server.NewConnector(bridgeID); err == nil {
			t.Errorf("NewConnector(%q) succeeded, want an error", bridgeID)
		}
	}
	// A bridge must not be able to impersonate the user with its bot or ghosts.
	for localpart, bridgeID := range map[string]string{"telegrambot": "telegram", "signal_alice": "signal"} {
		server := newServer(t, localmatrix.Options{UserLocalpart: localpart})
		if _, err := server.NewConnector(bridgeID); err == nil {
			t.Errorf("NewConnector(%q) for user %s succeeded, want an error", bridgeID, server.UserID())
		}
	}
}

func TestGhostIDRoundTrip(t *testing.T) {
	server := newServer(t, localmatrix.Options{})
	echo := newConnector(t, server, "echo")
	other := newConnector(t, server, "other")
	for _, remoteID := range []networkid.UserID{"123456", "Alice Smith", "user/with:odd@chars_\u00fc", "_"} {
		mxid := echo.FormatGhostMXID(remoteID)
		if _, _, err := mxid.Parse(); err != nil {
			t.Errorf("FormatGhostMXID(%q) = %q, which is not a valid user ID: %v", remoteID, mxid, err)
		}
		if got, ok := echo.ParseGhostMXID(mxid); !ok || got != remoteID {
			t.Errorf("ParseGhostMXID(%q) = %q, %v; want %q", mxid, got, ok, remoteID)
		}
		if _, ok := other.ParseGhostMXID(mxid); ok {
			t.Errorf("bridge other claims the ghost %q of bridge echo", mxid)
		}
	}
	for _, mxid := range []id.UserID{server.UserID(), "@echobot:" + id.UserID(server.ServerName()), "@echo_x:example.org"} {
		if got, ok := echo.ParseGhostMXID(mxid); ok {
			t.Errorf("ParseGhostMXID(%q) = %q, want no ghost", mxid, got)
		}
	}
}

func TestDeterministicIDs(t *testing.T) {
	server := newServer(t, localmatrix.Options{})
	echo := newConnector(t, server, "echo")
	other := newConnector(t, server, "other")
	key := networkid.PortalKey{ID: "chat", Receiver: "alice"}
	if echo.GenerateDeterministicRoomID(key) != echo.GenerateDeterministicRoomID(key) {
		t.Error("the room ID of a portal is not stable")
	}
	distinct := map[id.RoomID]string{}
	for name, roomID := range map[string]id.RoomID{
		"base":           echo.GenerateDeterministicRoomID(key),
		"other receiver": echo.GenerateDeterministicRoomID(networkid.PortalKey{ID: "chat", Receiver: "bob"}),
		"other chat":     echo.GenerateDeterministicRoomID(networkid.PortalKey{ID: "chat2", Receiver: "alice"}),
		"other bridge":   other.GenerateDeterministicRoomID(key),
	} {
		if previous, clash := distinct[roomID]; clash {
			t.Errorf("%s and %s have the same room ID %s", name, previous, roomID)
		}
		distinct[roomID] = name
	}
}

// createDM creates a direct room between the user and a ghost, as bridgev2
// does for a new portal.
func createDM(t *testing.T, server *localmatrix.Server, conn *localmatrix.Connector, ghost networkid.UserID) id.RoomID {
	t.Helper()
	roomID, err := conn.BotIntent().CreateRoom(t.Context(), &mautrix.ReqCreateRoom{
		Name:                 "Ghost",
		Preset:               "private_chat",
		IsDirect:             true,
		BeeperLocalRoomID:    conn.GenerateDeterministicRoomID(networkid.PortalKey{ID: networkid.PortalID(ghost)}),
		BeeperInitialMembers: []id.UserID{server.UserID(), conn.FormatGhostMXID(ghost)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return roomID
}

func TestCreateRoom(t *testing.T) {
	server := newServer(t, localmatrix.Options{})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	ghost := conn.FormatGhostMXID("alice")
	if err := conn.GhostIntent("alice").SetDisplayName(ctx, "Alice"); err != nil {
		t.Fatal(err)
	}
	roomID := createDM(t, server, conn, "alice")

	room, err := server.Room(ctx, roomID)
	if err != nil || room.BridgeID != "echo" || !room.IsDirect {
		t.Fatalf("Room = %+v, %v; want a direct room of bridge echo", room, err)
	}
	members, err := server.Members(ctx, roomID)
	if err != nil {
		t.Fatal(err)
	}
	bot := conn.BotIntent().GetMXID()
	for _, userID := range []id.UserID{bot, server.UserID(), ghost} {
		if members[userID] == nil || members[userID].Membership != event.MembershipJoin {
			t.Errorf("member %s = %+v, want joined", userID, members[userID])
		}
	}
	if members[ghost] != nil && members[ghost].Displayname != "Alice" {
		t.Errorf("ghost display name = %q, want the profile name", members[ghost].Displayname)
	}
	power, err := conn.GetPowerLevels(ctx, roomID)
	if err != nil || power.GetUserLevel(bot) != 100 {
		t.Errorf("bot power level = %v (%v), want 100", power, err)
	}
	joinRules, err := server.State(ctx, roomID, event.StateJoinRules, "")
	if err != nil || joinRules == nil || joinRules.Content.AsJoinRules().JoinRule != event.JoinRuleInvite {
		t.Errorf("join rules = %v (%v), want invite", joinRules, err)
	}

	// A room left behind by an interrupted portal creation is reused when
	// bridgev2 creates the portal again: the new state applies, and nobody
	// joins twice.
	memberEvents := func() int {
		events, err := server.Timeline(ctx, roomID, 0, 1000)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, evt := range events {
			if evt.Type == event.StateMember {
				n++
			}
		}
		return n
	}
	joinsBefore := memberEvents()
	again, err := conn.BotIntent().CreateRoom(ctx, &mautrix.ReqCreateRoom{
		Name:                 "Ghost again",
		IsDirect:             true,
		BeeperLocalRoomID:    roomID,
		BeeperInitialMembers: []id.UserID{server.UserID(), ghost},
	})
	if err != nil || again != roomID {
		t.Fatalf("creating the room again = %s, %v; want the same room", again, err)
	}
	if name, err := server.State(ctx, roomID, event.StateRoomName, ""); err != nil || name == nil || name.Content.AsRoomName().Name != "Ghost again" {
		t.Errorf("room name after the new creation = %v (%v)", name, err)
	}
	if joinsAfter := memberEvents(); joinsAfter != joinsBefore {
		t.Errorf("member events: %d, then %d after the new creation; want no new join", joinsBefore, joinsAfter)
	}

	// Room IDs are not shared between bridges, nor taken by a creation
	// without a deterministic ID.
	other := newConnector(t, server, "other")
	if _, err = other.BotIntent().CreateRoom(ctx, &mautrix.ReqCreateRoom{BeeperLocalRoomID: roomID}); err == nil {
		t.Error("another bridge took over an existing room")
	}
}

func TestTimelineAndRedaction(t *testing.T) {
	server := newServer(t, localmatrix.Options{})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	roomID := createDM(t, server, conn, "alice")
	intent := conn.GhostIntent("alice")

	var sent []id.EventID
	for _, body := range []string{"first", "second", "third"} {
		resp, err := intent.SendMessage(ctx, roomID, event.EventMessage, &event.Content{
			Parsed: &event.MessageEventContent{MsgType: event.MsgText, Body: body},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		sent = append(sent, resp.EventID)
	}
	events, err := server.Timeline(ctx, roomID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var messages []*event.Event
	for i, evt := range events {
		if i > 0 && evt.Unsigned.BeeperHSOrder <= events[i-1].Unsigned.BeeperHSOrder {
			t.Errorf("stream order %d after %d", evt.Unsigned.BeeperHSOrder, events[i-1].Unsigned.BeeperHSOrder)
		}
		if evt.Type == event.EventMessage {
			messages = append(messages, evt)
		}
	}
	if len(messages) != 3 || messages[0].ID != sent[0] || messages[2].Content.AsMessage().Body != "third" {
		t.Fatalf("messages = %v, want first, second, third", messages)
	}
	// Paging: what comes after the first message.
	after, err := server.Timeline(ctx, roomID, messages[0].Unsigned.BeeperHSOrder, 1)
	if err != nil || len(after) != 1 || after[0].ID != sent[1] {
		t.Errorf("Timeline after the first message = %v (%v), want the second one", after, err)
	}

	_, err = intent.SendMessage(ctx, roomID, event.EventRedaction, &event.Content{
		Parsed: &event.RedactionEventContent{Redacts: sent[1]},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	redacted, err := intent.GetEvent(ctx, roomID, sent[1])
	if err != nil {
		t.Fatal(err)
	}
	if body := redacted.Content.AsMessage().Body; body != "" {
		t.Errorf("the redacted message still has the body %q", body)
	}
	kept, err := intent.GetEvent(ctx, roomID, sent[2])
	if err != nil || kept.Content.AsMessage().Body != "third" {
		t.Errorf("the redaction erased another message: %v (%v)", kept, err)
	}
}

func TestMessagesBeforeAndEvent(t *testing.T) {
	server := newServer(t, localmatrix.Options{})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	roomID := createDM(t, server, conn, "alice")
	intent := conn.GhostIntent("alice")

	var sent []id.EventID
	for _, body := range []string{"1", "2", "3", "4", "5"} {
		resp, err := intent.SendMessage(ctx, roomID, event.EventMessage, &event.Content{
			Parsed: &event.MessageEventContent{MsgType: event.MsgText, Body: body},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		sent = append(sent, resp.EventID)
	}
	// The room's state events (creation, members, name) are not messages.
	latest, err := server.MessagesBefore(ctx, roomID, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 2 || latest[0].ID != sent[4] || latest[1].ID != sent[3] {
		t.Fatalf("latest two = %v, want 5 then 4", latest)
	}
	older, err := server.MessagesBefore(ctx, roomID, latest[1].Unsigned.BeeperHSOrder, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(older) != 3 || older[0].ID != sent[2] || older[2].ID != sent[0] {
		t.Fatalf("older = %v, want 3, 2, 1", older)
	}
	for _, evt := range append(latest, older...) {
		if evt.Type != event.EventMessage {
			t.Errorf("MessagesBefore returned a %s event", evt.Type.Type)
		}
	}

	evt, err := server.Event(ctx, roomID, sent[1])
	if err != nil || evt.Content.AsMessage().Body != "2" {
		t.Errorf("Event = %v (%v), want the message 2", evt, err)
	}
	if _, err = server.Event(ctx, roomID, "$nope"); !errors.Is(err, localmatrix.ErrNotFound) {
		t.Errorf("Event of an unknown ID: %v, want ErrNotFound", err)
	}
}

func TestUserSendRequiresMembershipAndBridge(t *testing.T) {
	server := newServer(t, localmatrix.Options{})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	content := &event.MessageEventContent{MsgType: event.MsgText, Body: "hello"}

	if _, err := server.SendMessage(ctx, "!unknown:"+id.RoomID(server.ServerName()), content); !errors.Is(err, localmatrix.ErrNotFound) {
		t.Errorf("sending to an unknown room: %v, want %v", err, localmatrix.ErrNotFound)
	}

	// A room the user is not in.
	roomID, err := conn.BotIntent().CreateRoom(ctx, &mautrix.ReqCreateRoom{Name: "Not mine"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.SendMessage(ctx, roomID, content); !errors.Is(err, localmatrix.ErrForbidden) {
		t.Errorf("sending to a room the user is not in: %v, want %v", err, localmatrix.ErrForbidden)
	}

	// The connector was never given to a bridgev2.Bridge.
	dm := createDM(t, server, conn, "alice")
	if _, err = server.SendMessage(ctx, dm, content); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("sending without a running bridge: %v, want a 'not running' error", err)
	}
}

func TestUserIntentIsTheDoublePuppet(t *testing.T) {
	server := newServer(t, localmatrix.Options{})
	conn := newConnector(t, server, "echo")
	intent, token, err := conn.NewUserIntent(t.Context(), server.UserID(), "token")
	if err != nil || !intent.IsDoublePuppet() || intent.GetMXID() != server.UserID() || token != "token" {
		t.Errorf("NewUserIntent(user) = %v, %q, %v; want the user's double puppet", intent, token, err)
	}
	if _, _, err = conn.NewUserIntent(t.Context(), "@someone:example.org", "token"); err == nil {
		t.Error("NewUserIntent accepted another user")
	}
}

func TestMediaRoundTrip(t *testing.T) {
	server := newServer(t, localmatrix.Options{})
	conn := newConnector(t, server, "echo")
	ctx := t.Context()
	intent := conn.GhostIntent("alice")
	data := []byte("not really a picture")
	uri, file, err := intent.UploadMedia(ctx, "", data, "picture.jpg", "image/jpeg")
	if err != nil || file != nil {
		t.Fatalf("UploadMedia = %q, %v, %v", uri, file, err)
	}
	if parsed, err := uri.Parse(); err != nil || parsed.Homeserver != server.ServerName() {
		t.Errorf("media URI %q (%v) is not a local mxc URI", uri, err)
	}
	got, err := intent.DownloadMedia(ctx, uri, nil)
	if err != nil || string(got) != string(data) {
		t.Errorf("DownloadMedia = %q, %v; want the uploaded data", got, err)
	}
	if _, err = intent.DownloadMedia(ctx, uri, &event.EncryptedFileInfo{}); !errors.Is(err, localmatrix.ErrEncryptedMediaUnsupported) {
		t.Errorf("downloading encrypted media: %v, want %v", err, localmatrix.ErrEncryptedMediaUnsupported)
	}
	if _, err = conn.GenerateContentURI(ctx, networkid.MediaID("media")); !errors.Is(err, bridgev2.ErrDirectMediaNotEnabled) {
		t.Errorf("GenerateContentURI: %v, want %v", err, bridgev2.ErrDirectMediaNotEnabled)
	}
}

func TestSubscribe(t *testing.T) {
	server := newServer(t, localmatrix.Options{})
	conn := newConnector(t, server, "echo")
	fast, unsubscribeFast := server.Subscribe(100)
	defer unsubscribeFast()
	slow, unsubscribeSlow := server.Subscribe(1)
	defer unsubscribeSlow()

	roomID := createDM(t, server, conn, "alice") // stores several events at once

	var received int
	for len(fast) > 0 {
		update := <-fast
		if update.Event == nil || update.Event.RoomID != roomID {
			t.Errorf("unexpected update %+v", update)
		}
		received++
	}
	events, err := server.Timeline(t.Context(), roomID, 0, 100)
	if err != nil || received != len(events) {
		t.Errorf("the subscriber received %d updates for %d stored events (%v)", received, len(events), err)
	}

	// The slow subscriber got the first update, then was dropped.
	if _, ok := <-slow; !ok {
		t.Fatal("the slow subscriber did not get the first update")
	}
	if _, ok := <-slow; ok {
		t.Error("the slow subscriber was not dropped")
	}
	unsubscribeSlow() // must not panic on a dropped subscriber
}

func TestUpgradeClearsPreviousBridgeStates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.db")
	open := func() *localmatrix.Server {
		db, err := sqlite.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		server := localmatrix.New(db, localmatrix.Options{Log: zerolog.Nop()})
		if err = server.Upgrade(t.Context()); err != nil {
			t.Fatal(err)
		}
		return server
	}
	first := open()
	conn := newConnector(t, first, "echo")
	state := &localmatrix.BridgeState{}
	state.StateEvent = "CONNECTED"
	state.RemoteID = "alice"
	if err := conn.SendBridgeStatus(t.Context(), &state.BridgeState); err != nil {
		t.Fatal(err)
	}
	if got, err := first.BridgeState(t.Context(), "echo", "alice"); err != nil || got == nil {
		t.Fatalf("BridgeState = %v, %v; want the stored state", got, err)
	}
	second := open()
	if got, err := second.BridgeState(t.Context(), "echo", "alice"); err != nil || got != nil {
		t.Errorf("BridgeState after restart = %v, %v; want none", got, err)
	}
}

// TestStopDropsBridgeStates checks that a bridge state sent after Stop
// succeeds without touching the database, which may already be closed:
// bridgev2 would retry a failed send forever.
func TestStopDropsBridgeStates(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	server := localmatrix.New(db, localmatrix.Options{Log: zerolog.Nop()})
	if err = server.Upgrade(t.Context()); err != nil {
		t.Fatal(err)
	}
	conn := newConnector(t, server, "echo")
	server.Stop()
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	state := &localmatrix.BridgeState{}
	state.StateEvent = "CONNECTED"
	state.RemoteID = "alice"
	if err = conn.SendBridgeStatus(t.Context(), &state.BridgeState); err != nil {
		t.Errorf("SendBridgeStatus after Stop = %v, want nil", err)
	}
}
