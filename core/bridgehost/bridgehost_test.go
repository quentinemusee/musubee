// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgehost_test

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/bridgehost"
	"github.com/quentinemusee/musubee/core/connector/echo"
)

const (
	echoBridge networkid.BridgeID = "echo"
	// echoDelay is short to keep the tests fast, but long enough to tell a
	// delayed echo from an instant one.
	echoDelay = 500 * time.Millisecond
	// waitTimeout bounds every wait; the operations take milliseconds.
	waitTimeout = 15 * time.Second
)

// syncBuffer collects the logs of every goroutine of the bridge.
type syncBuffer struct {
	lock sync.Mutex
	buf  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.buf.String()
}

type harness struct {
	t       testing.TB
	host    *bridgehost.Host
	logs    *syncBuffer
	stopped bool
}

// startHost starts a host with the given networks on the database at path,
// with every log level enabled so that tests can inspect the logs.
func startHost(t testing.TB, path string, bridgeIDs ...networkid.BridgeID) *harness {
	t.Helper()
	return startHostWithLogLevel(t, zerolog.TraceLevel, path, bridgeIDs...)
}

func startHostWithLogLevel(t testing.TB, level zerolog.Level, path string, bridgeIDs ...networkid.BridgeID) *harness {
	t.Helper()
	logs := &syncBuffer{}
	host, err := bridgehost.New(bridgehost.Options{
		DatabasePath: path,
		Log:          zerolog.New(logs).Level(level),
	})
	if err != nil {
		t.Fatalf("creating the host: %v", err)
	}
	for _, bridgeID := range bridgeIDs {
		if _, err = host.AddNetwork(bridgeID, echo.New(echo.Config{EchoDelay: echoDelay})); err != nil {
			t.Fatalf("adding network %s: %v", bridgeID, err)
		}
	}
	if err = host.Start(t.Context()); err != nil {
		t.Fatalf("starting the host: %v", err)
	}
	h := &harness{t: t, host: host, logs: logs}
	t.Cleanup(h.stop)
	return h
}

// stop stops the host; it is called by the test cleanup if the test did not.
func (h *harness) stop() {
	if h.stopped {
		return
	}
	h.stopped = true
	if err := h.host.Stop(); err != nil {
		h.t.Errorf("stopping the host: %v", err)
	}
}

// waitFor polls cond until it returns true, an error, or the timeout.
func waitFor(t testing.TB, what string, cond func() (bool, error)) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for {
		ok, err := cond()
		if err != nil {
			t.Fatalf("waiting for %s: %v", what, err)
		}
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", waitTimeout, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// login goes through the echo login flow, as the application will.
func (h *harness) login(bridgeID networkid.BridgeID, username string) *bridgev2.UserLogin {
	h.t.Helper()
	ctx := h.t.Context()
	br := h.host.Bridge(bridgeID)
	user, err := h.host.User(ctx, bridgeID)
	if err != nil {
		h.t.Fatalf("getting the user: %v", err)
	}
	flows := br.Network.GetLoginFlows()
	if len(flows) != 1 || flows[0].ID != echo.FlowUsername {
		h.t.Fatalf("unexpected login flows: %+v", flows)
	}
	process, err := br.Network.CreateLogin(ctx, user, echo.FlowUsername)
	if err != nil {
		h.t.Fatalf("creating the login: %v", err)
	}
	step, err := process.Start(ctx)
	if err != nil {
		h.t.Fatalf("starting the login: %v", err)
	}
	if step.Type != bridgev2.LoginStepTypeUserInput || len(step.UserInputParams.Fields) != 1 {
		h.t.Fatalf("unexpected first login step: %+v", step)
	}
	field := step.UserInputParams.Fields[0].ID
	step, err = process.(bridgev2.LoginProcessUserInput).SubmitUserInput(ctx, map[string]string{field: username})
	if err != nil {
		h.t.Fatalf("submitting the username: %v", err)
	}
	if step.Type != bridgev2.LoginStepTypeComplete || step.CompleteParams.UserLogin == nil {
		h.t.Fatalf("unexpected last login step: %+v", step)
	}
	login := step.CompleteParams.UserLogin
	h.waitConnected(bridgeID, login.ID)
	return login
}

func (h *harness) waitConnected(bridgeID networkid.BridgeID, loginID networkid.UserLoginID) {
	h.t.Helper()
	waitFor(h.t, "the login to be connected", func() (bool, error) {
		state, err := h.host.Matrix.BridgeState(h.t.Context(), string(bridgeID), string(loginID))
		return state != nil && state.StateEvent == status.StateConnected, err
	})
}

// room returns the room of the conversation with a contact, once it exists.
func (h *harness) room(bridgeID networkid.BridgeID, loginID networkid.UserLoginID, contact networkid.UserID) id.RoomID {
	h.t.Helper()
	key := networkid.PortalKey{ID: networkid.PortalID(contact), Receiver: loginID}
	roomID := h.host.Bridge(bridgeID).Matrix.GenerateDeterministicRoomID(key)
	waitFor(h.t, "the room with "+string(contact), func() (bool, error) {
		members, err := h.host.Matrix.Members(h.t.Context(), roomID)
		if err != nil {
			return false, err
		}
		ghost := h.host.Bridge(bridgeID).Matrix.FormatGhostMXID(contact)
		return members[ghost] != nil && members[ghost].Membership == event.MembershipJoin, nil
	})
	return roomID
}

// allRooms waits for the room of every echo contact of a login: the
// connector announces them together, but bridgev2 creates them in parallel.
func (h *harness) allRooms(bridgeID networkid.BridgeID, loginID networkid.UserLoginID) {
	h.t.Helper()
	for _, contact := range echo.Contacts {
		h.room(bridgeID, loginID, contact.ID)
	}
}

func (h *harness) send(roomID id.RoomID, text string) id.EventID {
	h.t.Helper()
	eventID, err := h.host.Matrix.SendMessage(h.t.Context(), roomID, &event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    text,
	})
	if err != nil {
		h.t.Fatalf("sending %q: %v", text, err)
	}
	return eventID
}

// messages returns the text messages of a room, by sender.
func (h *harness) messages(roomID id.RoomID) ([]*event.Event, error) {
	events, err := h.host.Matrix.Timeline(h.t.Context(), roomID, 0, 1000)
	if err != nil {
		return nil, err
	}
	var messages []*event.Event
	for _, evt := range events {
		if evt.Type == event.EventMessage {
			messages = append(messages, evt)
		}
	}
	return messages, nil
}

func (h *harness) waitForMessageFrom(roomID id.RoomID, sender id.UserID, text string) *event.Event {
	h.t.Helper()
	var found *event.Event
	waitFor(h.t, fmt.Sprintf("%q from %s", text, sender), func() (bool, error) {
		messages, err := h.messages(roomID)
		for _, evt := range messages {
			if evt.Sender == sender && evt.Content.AsMessage().Body == text {
				found = evt
			}
		}
		return found != nil, err
	})
	return found
}

func (h *harness) waitForStatus(eventID id.EventID, want event.MessageStatus) {
	h.t.Helper()
	waitFor(h.t, "message status "+string(want), func() (bool, error) {
		st, err := h.host.Matrix.MessageStatus(h.t.Context(), eventID)
		if err != nil || st == nil || st.Status == event.MessageStatusPending {
			return false, err
		}
		if st.Status != want {
			return false, fmt.Errorf("status is %s (%s: %s), want %s", st.Status, st.Reason, st.Message, want)
		}
		return true, nil
	})
}

func (h *harness) bridgedMessageCount(bridgeID networkid.BridgeID, loginID networkid.UserLoginID, contact networkid.UserID) int {
	h.t.Helper()
	var count int
	err := h.host.DB.QueryRow(h.t.Context(),
		`SELECT COUNT(*) FROM message WHERE bridge_id=$1 AND room_id=$2 AND room_receiver=$3`,
		bridgeID, contact, loginID).Scan(&count)
	if err != nil {
		h.t.Fatalf("counting bridged messages: %v", err)
	}
	return count
}

// TestMessageGoesThroughTheConnectorToLocalStorage is the acceptance test of
// T1.1: a message goes through a bridgev2 connector and its echo reaches the
// local storage, without any homeserver.
func TestMessageGoesThroughTheConnectorToLocalStorage(t *testing.T) {
	h := startHost(t, filepath.Join(t.TempDir(), "core.db"), echoBridge)
	ctx := t.Context()
	login := h.login(echoBridge, "alice")

	// The login creates one direct conversation per contact.
	for _, contact := range echo.Contacts {
		roomID := h.room(echoBridge, login.ID, contact.ID)
		name, err := h.host.Matrix.State(ctx, roomID, event.StateRoomName, "")
		if err != nil || name == nil || name.Content.AsRoomName().Name != contact.Name {
			t.Errorf("room %s: name = %+v (%v), want %q", roomID, name, err, contact.Name)
		}
		room, err := h.host.Matrix.Room(ctx, roomID)
		if err != nil || !room.IsDirect || room.BridgeID != string(echoBridge) {
			t.Errorf("room %s = %+v (%v), want a direct room of bridge %s", roomID, room, err, echoBridge)
		}
		ghost := h.host.Bridge(echoBridge).Matrix.FormatGhostMXID(contact.ID)
		profile, err := h.host.Matrix.Profile(ctx, ghost)
		if err != nil || profile.DisplayName != contact.Name {
			t.Errorf("profile of %s = %+v (%v), want %q", ghost, profile, err, contact.Name)
		}
	}
	rooms, err := h.host.Matrix.Rooms(ctx)
	if err != nil || len(rooms) != len(echo.Contacts) {
		t.Fatalf("rooms = %d (%v), want %d", len(rooms), err, len(echo.Contacts))
	}

	roomID := h.room(echoBridge, login.ID, echo.ContactInstant)
	const text = "Hello through bridgev2, without a homeserver"
	sent := h.send(roomID, text)
	h.waitForStatus(sent, event.MessageStatusSuccess)
	ghost := h.host.Bridge(echoBridge).Matrix.FormatGhostMXID(echo.ContactInstant)
	echoed := h.waitForMessageFrom(roomID, ghost, text)

	// Both messages are in the local storage, in order: the user's, then the echo.
	messages, err := h.messages(roomID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].ID != sent || messages[0].Sender != h.host.Matrix.UserID() || messages[1].ID != echoed.ID {
		t.Fatalf("timeline = %v, want [%s from the user, %s from %s]", eventSummary(messages), sent, echoed.ID, ghost)
	}
	if messages[0].Unsigned.BeeperHSOrder >= messages[1].Unsigned.BeeperHSOrder {
		t.Errorf("stream orders %d, %d are not increasing", messages[0].Unsigned.BeeperHSOrder, messages[1].Unsigned.BeeperHSOrder)
	}
	// bridgev2 keeps its own mapping of both messages to network message IDs.
	if n := h.bridgedMessageCount(echoBridge, login.ID, echo.ContactInstant); n != 2 {
		t.Errorf("bridgev2 message table has %d rows for the conversation, want 2", n)
	}
}

func eventSummary(events []*event.Event) string {
	parts := make([]string, len(events))
	for i, evt := range events {
		parts[i] = fmt.Sprintf("%s from %s", evt.ID, evt.Sender)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func TestDelayedEcho(t *testing.T) {
	h := startHost(t, filepath.Join(t.TempDir(), "core.db"), echoBridge)
	login := h.login(echoBridge, "alice")
	roomID := h.room(echoBridge, login.ID, echo.ContactDelayed)
	ghost := h.host.Bridge(echoBridge).Matrix.FormatGhostMXID(echo.ContactDelayed)

	start := time.Now()
	sent := h.send(roomID, "Are you there?")
	h.waitForStatus(sent, event.MessageStatusSuccess)
	if elapsed := time.Since(start); elapsed < echoDelay {
		messages, _ := h.messages(roomID)
		for _, evt := range messages {
			if evt.Sender == ghost {
				t.Fatalf("the echo arrived after %s, before the %s delay", elapsed, echoDelay)
			}
		}
	}
	h.waitForMessageFrom(roomID, ghost, "Are you there?")
	if elapsed := time.Since(start); elapsed < echoDelay {
		t.Errorf("the echo arrived after %s, want at least %s", elapsed, echoDelay)
	}
}

func TestFailedSendIsReported(t *testing.T) {
	h := startHost(t, filepath.Join(t.TempDir(), "core.db"), echoBridge)
	ctx := t.Context()
	login := h.login(echoBridge, "alice")
	roomID := h.room(echoBridge, login.ID, echo.ContactUnreachable)

	sent := h.send(roomID, "Nobody will read this")
	h.waitForStatus(sent, event.MessageStatusFail)
	st, err := h.host.Matrix.MessageStatus(ctx, sent)
	if err != nil {
		t.Fatal(err)
	}
	if st.Reason != event.MessageStatusNetworkError || !strings.Contains(st.Message, "unreachable") {
		t.Errorf("status = %+v, want a network error explaining that the contact is unreachable", st)
	}
	// The message stays in the timeline (marked as failed), nothing comes back,
	// and bridgev2 did not record it as bridged.
	time.Sleep(2 * echoDelay)
	messages, err := h.messages(roomID)
	if err != nil || len(messages) != 1 || messages[0].ID != sent {
		t.Errorf("timeline = %s (%v), want only the failed message", eventSummary(messages), err)
	}
	if n := h.bridgedMessageCount(echoBridge, login.ID, echo.ContactUnreachable); n != 0 {
		t.Errorf("bridgev2 message table has %d rows for the conversation, want 0", n)
	}
}

func TestInvalidUsernameIsRejected(t *testing.T) {
	h := startHost(t, filepath.Join(t.TempDir(), "core.db"), echoBridge)
	ctx := t.Context()
	br := h.host.Bridge(echoBridge)
	user, err := h.host.User(ctx, echoBridge)
	if err != nil {
		t.Fatal(err)
	}
	process, err := br.Network.CreateLogin(ctx, user, echo.FlowUsername)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = process.Start(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = process.(bridgev2.LoginProcessUserInput).SubmitUserInput(ctx, map[string]string{"username": "Not Valid!"})
	if !errors.Is(err, echo.ErrInvalidUsername) {
		t.Errorf("error = %v, want %v", err, echo.ErrInvalidUsername)
	}
	if logins := user.GetUserLogins(); len(logins) != 0 {
		t.Errorf("logins = %d, want none", len(logins))
	}
}

// TestRestartKeepsHistoryAndLogin stops the host and starts a new one on the
// same database, as when the app is closed and reopened.
func TestRestartKeepsHistoryAndLogin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.db")
	h := startHost(t, path, echoBridge)
	login := h.login(echoBridge, "alice")
	roomID := h.room(echoBridge, login.ID, echo.ContactInstant)
	ghost := h.host.Bridge(echoBridge).Matrix.FormatGhostMXID(echo.ContactInstant)
	h.waitForStatus(h.send(roomID, "before restart"), event.MessageStatusSuccess)
	h.waitForMessageFrom(roomID, ghost, "before restart")
	h.allRooms(echoBridge, login.ID)
	h.stop()

	h = startHost(t, path, echoBridge)
	// The state stored by the first process was cleared on start: this waits
	// for the login to reconnect, which announces every conversation again.
	h.waitConnected(echoBridge, login.ID)
	h.allRooms(echoBridge, login.ID)
	rooms, err := h.host.Matrix.Rooms(t.Context())
	if err != nil || len(rooms) != len(echo.Contacts) {
		t.Fatalf("rooms after restart = %d (%v), want %d (no duplicates)", len(rooms), err, len(echo.Contacts))
	}
	h.waitForStatus(h.send(roomID, "after restart"), event.MessageStatusSuccess)
	h.waitForMessageFrom(roomID, ghost, "after restart")
	messages, err := h.messages(roomID)
	if err != nil || len(messages) != 4 {
		t.Errorf("timeline after restart = %s (%v), want 4 messages", eventSummary(messages), err)
	}
}

// TestTwoNetworksShareOneProcess runs two bridges side by side on the same
// local storage, as the app will run Telegram, Signal, ... together.
func TestTwoNetworksShareOneProcess(t *testing.T) {
	h := startHost(t, filepath.Join(t.TempDir(), "core.db"), "echoa", "echob")
	loginA := h.login("echoa", "alice")
	loginB := h.login("echob", "alice")
	roomA := h.room("echoa", loginA.ID, echo.ContactInstant)
	roomB := h.room("echob", loginB.ID, echo.ContactInstant)
	if roomA == roomB {
		t.Fatalf("both networks use room %s", roomA)
	}
	ghostA := h.host.Bridge("echoa").Matrix.FormatGhostMXID(echo.ContactInstant)
	ghostB := h.host.Bridge("echob").Matrix.FormatGhostMXID(echo.ContactInstant)
	if ghostA == ghostB {
		t.Fatalf("both networks use ghost %s", ghostA)
	}
	h.waitForStatus(h.send(roomA, "to network A"), event.MessageStatusSuccess)
	h.waitForStatus(h.send(roomB, "to network B"), event.MessageStatusSuccess)
	h.waitForMessageFrom(roomA, ghostA, "to network A")
	h.waitForMessageFrom(roomB, ghostB, "to network B")
	for roomID, want := range map[id.RoomID]string{roomA: "to network A", roomB: "to network B"} {
		messages, err := h.messages(roomID)
		if err != nil || len(messages) != 2 {
			t.Errorf("timeline of %s = %s (%v), want 2 messages", roomID, eventSummary(messages), err)
			continue
		}
		for _, evt := range messages {
			if body := evt.Content.AsMessage().Body; body != want {
				t.Errorf("room %s contains %q, which belongs to the other network", roomID, body)
			}
		}
	}
	h.allRooms("echoa", loginA.ID)
	h.allRooms("echob", loginB.ID)
	rooms, err := h.host.Matrix.Rooms(t.Context())
	if err != nil || len(rooms) != 2*len(echo.Contacts) {
		t.Errorf("rooms = %d (%v), want %d", len(rooms), err, 2*len(echo.Contacts))
	}
}

// TestMessageContentIsNotLogged checks rule 8 of CLAUDE.md ("no logging of
// message content") for the whole path, bridgev2 included, at the most
// verbose log level.
func TestMessageContentIsNotLogged(t *testing.T) {
	h := startHost(t, filepath.Join(t.TempDir(), "core.db"), echoBridge)
	login := h.login(echoBridge, "alice")
	ghost := h.host.Bridge(echoBridge).Matrix.FormatGhostMXID(echo.ContactInstant)
	const secret = "s3cret-content-that-must-not-be-logged"
	// loggedIDs are events whose handling bridgev2 logs (with their ID): the
	// echo coming from the network, and the message the network rejected.
	var loggedIDs []id.EventID
	instant := h.room(echoBridge, login.ID, echo.ContactInstant)
	h.waitForStatus(h.send(instant, secret), event.MessageStatusSuccess)
	loggedIDs = append(loggedIDs, h.waitForMessageFrom(instant, ghost, secret).ID)
	unreachable := h.room(echoBridge, login.ID, echo.ContactUnreachable)
	failed := h.send(unreachable, secret)
	h.waitForStatus(failed, event.MessageStatusFail)
	loggedIDs = append(loggedIDs, failed)
	h.stop()
	logs := h.logs.String()
	// Positive control: the logs do cover the handling of these messages,
	// so the absence of the content below means something.
	for _, eventID := range loggedIDs {
		if !strings.Contains(logs, string(eventID)) {
			t.Fatalf("the logs never mention event %s: the check would prove nothing", eventID)
		}
	}
	if strings.Contains(logs, secret) {
		t.Errorf("the message content appears in the logs")
	}
}

// TestStoppingOneNetworkLeavesTheOthersRunning stops one bridge while the
// app keeps running: the shared database stays open for the other network,
// and a message sent to the stopped network fails instead of staying pending.
func TestStoppingOneNetworkLeavesTheOthersRunning(t *testing.T) {
	h := startHost(t, filepath.Join(t.TempDir(), "core.db"), "echoa", "echob")
	loginA := h.login("echoa", "alice")
	loginB := h.login("echob", "alice")
	roomA := h.room("echoa", loginA.ID, echo.ContactInstant)
	roomB := h.room("echob", loginB.ID, echo.ContactInstant)

	h.host.Bridge("echoa").Stop()

	h.waitForStatus(h.send(roomA, "to the stopped network"), event.MessageStatusFail)
	h.waitForStatus(h.send(roomB, "to the running network"), event.MessageStatusSuccess)
	h.waitForMessageFrom(roomB, h.host.Bridge("echob").Matrix.FormatGhostMXID(echo.ContactInstant), "to the running network")
}
