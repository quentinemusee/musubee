// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package embedded

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

const eventTimeout = 15 * time.Second

func open(t *testing.T) *Core {
	t.Helper()
	cfg, err := json.Marshal(Config{DataDir: t.TempDir(), LogLevel: "debug", EchoDelayMS: 200})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		// Give goroutines that outlive Close a moment to show themselves.
		time.Sleep(50 * time.Millisecond)
		checkLateLogs(t, c.logs.lateLines())
	})
	return c
}

// checkLateLogs accepts, among the lines logged after Close, only those of
// the work bridgev2 aborts when it stops (see logWriter): failures caused by
// the cancellation, and anything else logged by an aborted portal event
// handler, unless it panicked.
func checkLateLogs(t *testing.T, lines []string) {
	t.Helper()
	for _, line := range lines {
		var entry struct {
			Message string `json:"message"`
			Error   string `json:"error"`
			Action  string `json:"action"`
		}
		_ = json.Unmarshal([]byte(line), &entry)
		aborted := strings.Contains(entry.Error, "context canceled") || strings.Contains(entry.Error, "database is closed")
		inHandler := strings.HasPrefix(entry.Action, "handle ")
		if !(aborted || inHandler) || strings.Contains(strings.ToLower(entry.Message), "panic") {
			t.Errorf("unexpected log line after Close: %s", line)
		}
	}
	if len(lines) > 0 {
		t.Logf("%d lines logged after Close, starting with: %s", len(lines), lines[0])
	}
}

// call sends a request and decodes the result into out; it fails the test
// when the core reports an error.
func call(t *testing.T, c *Core, command string, params, out any) {
	t.Helper()
	if err := callErr(c, command, params, out); err != nil {
		t.Fatalf("%s: %v", command, err)
	}
}

func callErr(c *Core, command string, params, out any) error {
	req := map[string]any{"id": 7, "command": command}
	if params != nil {
		req["params"] = params
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	var resp struct {
		ID     int64           `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err = json.Unmarshal(c.Call(data), &resp); err != nil {
		return fmt.Errorf("invalid response: %w", err)
	}
	if resp.ID != 7 {
		return fmt.Errorf("response ID %d, want 7", resp.ID)
	}
	if resp.Error != "" {
		return fmt.Errorf("%s", resp.Error)
	}
	if out != nil {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

type anyEvent struct {
	Type    string `json:"type"`
	RoomID  string `json:"room_id"`
	EventID string `json:"event_id"`
	FromMe  bool   `json:"from_me"`
	Body    string `json:"body"`
	Status  string `json:"status"`
	Message string `json:"message"`
	State   string `json:"state"`
}

// waitEvent reads events until one matches.
func waitEvent(t *testing.T, c *Core, what string, match func(anyEvent) bool) anyEvent {
	t.Helper()
	deadline := time.Now().Add(eventTimeout)
	for time.Now().Before(deadline) {
		data, ok := c.NextEvent(time.Until(deadline))
		if !ok {
			break
		}
		var evt anyEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			t.Fatalf("invalid event %s: %v", data, err)
		}
		if match(evt) {
			return evt
		}
	}
	t.Fatalf("no %s within %s", what, eventTimeout)
	return anyEvent{}
}

func login(t *testing.T, c *Core) loginResult {
	t.Helper()
	var result loginResult
	call(t, c, "login", map[string]string{"username": "alice"}, &result)
	return result
}

func roomNamed(t *testing.T, rooms []room, name string) string {
	t.Helper()
	for _, r := range rooms {
		if r.Name == name {
			return string(r.RoomID)
		}
	}
	t.Fatalf("no room named %q in %+v", name, rooms)
	return ""
}

func TestOpenRejectsInvalidConfig(t *testing.T) {
	for _, cfg := range []string{``, `{}`, `{"data_dir":`, `{"data_dir":"x","log_level":"loud"}`} {
		if c, err := Open([]byte(cfg)); err == nil {
			_ = c.Close()
			t.Errorf("Open(%q) succeeded, want an error", cfg)
		}
	}
}

func TestPing(t *testing.T) {
	c := open(t)
	var result struct {
		Payload string `json:"payload"`
	}
	call(t, c, "ping", map[string]string{"payload": "hi 👋"}, &result)
	if result.Payload != "hi 👋" {
		t.Errorf("ping payload = %q", result.Payload)
	}
}

func TestInvalidRequests(t *testing.T) {
	c := open(t)
	if out := string(c.Call([]byte(`{not json`))); !strings.Contains(out, `"error":"invalid request`) {
		t.Errorf("invalid JSON: %s", out)
	}
	if err := callErr(c, "teleport", nil, nil); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("unknown command: %v", err)
	}
	if err := callErr(c, "send", "not an object", nil); err == nil || !strings.Contains(err.Error(), "invalid params") {
		t.Errorf("invalid params: %v", err)
	}
	if err := callErr(c, "send", map[string]string{"room_id": "!nope:musubee.local", "text": "x"}, nil); err == nil {
		t.Error("sending to an unknown room succeeded")
	}
}

func TestLoginSendAndEcho(t *testing.T) {
	c := open(t)
	result := login(t, c)
	if result.LoginID == "" || len(result.Rooms) != 3 {
		t.Fatalf("login = %+v, want a login ID and 3 rooms", result)
	}
	waitEvent(t, c, "connected state", func(e anyEvent) bool { return e.Type == "network_state" && e.State == "CONNECTED" })

	roomID := roomNamed(t, result.Rooms, "Instant Echo")
	var sent struct {
		EventID string `json:"event_id"`
	}
	call(t, c, "send", map[string]string{"room_id": roomID, "text": "hello"}, &sent)
	waitEvent(t, c, "own message", func(e anyEvent) bool {
		return e.Type == "message" && e.EventID == sent.EventID && e.FromMe && e.Body == "hello"
	})
	waitEvent(t, c, "success status", func(e anyEvent) bool {
		return e.Type == "message_status" && e.EventID == sent.EventID && e.Status == "SUCCESS"
	})
	echo := waitEvent(t, c, "echo", func(e anyEvent) bool { return e.Type == "message" && !e.FromMe })
	if echo.RoomID != roomID || !strings.Contains(echo.Body, "hello") {
		t.Errorf("echo = %+v, want a message containing %q in %s", echo, "hello", roomID)
	}
}

func TestFailedSendIsAnEvent(t *testing.T) {
	c := open(t)
	roomID := roomNamed(t, login(t, c).Rooms, "Unreachable Contact")
	var sent struct {
		EventID string `json:"event_id"`
	}
	call(t, c, "send", map[string]string{"room_id": roomID, "text": "anyone?"}, &sent)
	failure := waitEvent(t, c, "failure status", func(e anyEvent) bool {
		return e.Type == "message_status" && e.EventID == sent.EventID && e.Status != "PENDING"
	})
	if failure.Status != "FAIL_PERMANENT" || failure.Message == "" {
		t.Errorf("status = %+v, want FAIL_PERMANENT with a message", failure)
	}
}

func TestStats(t *testing.T) {
	c := open(t)
	var s statsResult
	call(t, c, "stats", nil, &s)
	if s.HeapAllocBytes == 0 || s.Goroutines == 0 {
		t.Errorf("stats = %+v", s)
	}
}

func TestCloseEndsTheEventStream(t *testing.T) {
	c := open(t)
	login(t, c)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	waitEvent(t, c, "closed event", func(e anyEvent) bool { return e.Type == "closed" })
	if data, ok := c.NextEvent(time.Millisecond); !ok || string(data) != `{"type":"closed"}` {
		t.Errorf("event after the end = %s, %v; want closed again", data, ok)
	}
}

// TestReopenKeepsTheLogin opens a core twice on the same directory, as the
// application does on every launch.
func TestReopenKeepsTheLogin(t *testing.T) {
	dir := t.TempDir()
	cfg := []byte(fmt.Sprintf(`{"data_dir":%q}`, dir))
	c, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	login(t, c)
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	waitEvent(t, c, "reconnection", func(e anyEvent) bool { return e.Type == "network_state" && e.State == "CONNECTED" })
	var rooms []room
	call(t, c, "rooms", nil, &rooms)
	if len(rooms) != 3 {
		t.Errorf("rooms after reopening = %+v, want 3", rooms)
	}
}

// TestSlowReaderGetsAnOverflow checks that the core never blocks on an
// application that stops reading events.
func TestSlowReaderGetsAnOverflow(t *testing.T) {
	c := &Core{events: make(chan []byte, 4)}
	for i := range 10 {
		c.push([]byte(fmt.Sprintf(`{"n":%d}`, i)))
	}
	var got []string
	for len(c.events) > 0 {
		got = append(got, string(<-c.events))
	}
	if len(got) != 4 || got[len(got)-1] != `{"type":"overflow"}` {
		t.Errorf("queue = %v, want 4 events ending with an overflow", got)
	}
}
