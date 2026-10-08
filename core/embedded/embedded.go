// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package embedded is the core as an embedding application sees it: JSON
// requests in, JSON responses and events out. The C library (package ffi)
// is a thin layer over it, so that everything except the C calls is tested
// in Go.
//
// This is the T1.2 spike surface (docs/ADR/0010-core-shared-library.md):
// just enough commands to log in to the echo network and exchange messages.
// The real core/UI contract is designed in T1.4.
package embedded

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/bridgehost"
	"github.com/quentinemusee/musubee/core/connector/echo"
	"github.com/quentinemusee/musubee/core/localmatrix"
)

// echoBridge is the bridge ID of the echo network.
const echoBridge networkid.BridgeID = "echo"

// eventQueueSize bounds the events waiting for the application. When the
// application stops reading, the oldest events are not kept forever: the
// queue reports an overflow and the application must re-read the state.
const eventQueueSize = 1024

// Config is the JSON configuration passed to Open.
type Config struct {
	// DataDir holds the database and the log file. Required.
	DataDir string `json:"data_dir"`
	// LogLevel is a zerolog level name ("info" if empty).
	LogLevel string `json:"log_level,omitempty"`
	// EchoDelayMS is the delay of the echo network's delayed contact.
	EchoDelayMS int `json:"echo_delay_ms,omitempty"`
}

// Core is one running core.
type Core struct {
	host *bridgehost.Host
	logs *logWriter
	log  zerolog.Logger

	// ctx is cancelled by Close; it bounds every request.
	ctx    context.Context
	cancel context.CancelFunc

	events      chan []byte
	unsubscribe func()
	forwarding  sync.WaitGroup
	closeOnce   sync.Once
	closeErr    error
}

// Open starts a core from a JSON Config.
func Open(config []byte) (*Core, error) {
	var cfg Config
	if err := json.Unmarshal(config, &cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	if cfg.DataDir == "" {
		return nil, errors.New("invalid configuration: data_dir is required")
	}
	level := zerolog.InfoLevel
	if cfg.LogLevel != "" {
		var err error
		if level, err = zerolog.ParseLevel(cfg.LogLevel); err != nil {
			return nil, fmt.Errorf("invalid configuration: %w", err)
		}
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(filepath.Join(cfg.DataDir, "core.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	logs := &logWriter{f: logFile}
	log := zerolog.New(logs).Level(level).With().Timestamp().Logger()

	c := &Core{logs: logs, log: log, events: make(chan []byte, eventQueueSize)}
	c.ctx, c.cancel = context.WithCancel(log.WithContext(context.Background()))
	if err = c.start(cfg); err != nil {
		c.cancel()
		if c.host != nil {
			_ = c.host.Stop()
		}
		_ = logs.close()
		return nil, err
	}
	return c, nil
}

func (c *Core) start(cfg Config) error {
	host, err := bridgehost.New(bridgehost.Options{
		DatabasePath: filepath.Join(cfg.DataDir, "core.db"),
		Log:          c.log,
	})
	if err != nil {
		return err
	}
	c.host = host
	network := echo.New(echo.Config{EchoDelay: time.Duration(cfg.EchoDelayMS) * time.Millisecond})
	if _, err = host.AddNetwork(echoBridge, network); err != nil {
		return err
	}
	// Subscribe before starting, so that no event of the start is missed.
	updates, unsubscribe := host.Matrix.Subscribe(eventQueueSize)
	c.unsubscribe = unsubscribe
	c.forwarding.Add(1)
	go c.forward(updates)
	return host.Start(c.ctx)
}

// Close stops the core. It is safe to call more than once.
func (c *Core) Close() error {
	c.closeOnce.Do(func() {
		c.cancel()
		c.closeErr = c.host.Stop()
		c.unsubscribe()
		c.forwarding.Wait()
		close(c.events)
		if err := c.logs.close(); c.closeErr == nil {
			c.closeErr = err
		}
	})
	return c.closeErr
}

// NextEvent waits up to timeout for the next event. It returns false on
// timeout, and a final {"type":"closed"} event once the core is closed.
func (c *Core) NextEvent(timeout time.Duration) ([]byte, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case evt, ok := <-c.events:
		if !ok {
			return []byte(`{"type":"closed"}`), true
		}
		return evt, true
	case <-timer.C:
		return nil, false
	}
}

// Event types sent to the application.
type messageEvent struct {
	Type    string    `json:"type"`
	RoomID  id.RoomID `json:"room_id"`
	EventID string    `json:"event_id"`
	Sender  id.UserID `json:"sender"`
	FromMe  bool      `json:"from_me"`
	Body    string    `json:"body"`
}

type statusEvent struct {
	Type    string `json:"type"`
	EventID string `json:"event_id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type stateEvent struct {
	Type    string `json:"type"`
	Network string `json:"network"`
	LoginID string `json:"login_id,omitempty"`
	State   string `json:"state"`
}

func (c *Core) forward(updates <-chan localmatrix.Update) {
	defer c.forwarding.Done()
	me := c.host.Matrix.UserID()
	for update := range updates {
		var evt any
		switch {
		case update.Event != nil && update.Event.Type == event.EventMessage:
			content := update.Event.Content.AsMessage()
			evt = messageEvent{"message", update.Event.RoomID, string(update.Event.ID), update.Event.Sender, update.Event.Sender == me, content.Body}
		case update.MessageStatus != nil:
			evt = statusEvent{"message_status", string(update.MessageStatus.EventID), string(update.MessageStatus.Status), update.MessageStatus.Message}
		case update.BridgeState != nil:
			evt = stateEvent{"network_state", update.BridgeState.BridgeID, string(update.BridgeState.RemoteID), string(update.BridgeState.StateEvent)}
		default:
			continue
		}
		data, err := json.Marshal(evt)
		if err != nil {
			c.log.Err(err).Msg("Failed to encode an event for the application")
			continue
		}
		c.push(data)
	}
	// The channel closes when the core stops, or when this subscriber fell
	// behind and the server dropped it.
	if c.ctx.Err() == nil {
		c.push([]byte(`{"type":"overflow"}`))
		c.log.Warn().Msg("The application read events too slowly; it must re-read the state")
	}
}

// push queues an event without ever blocking the core: when the queue is
// full, the oldest event is dropped and an overflow is reported instead.
func (c *Core) push(data []byte) {
	for {
		select {
		case c.events <- data:
			return
		default:
		}
		select {
		case <-c.events:
			data = []byte(`{"type":"overflow"}`)
		default:
		}
	}
}

// request and response are the JSON envelope of Call.
type request struct {
	ID      int64           `json:"id"`
	Command string          `json:"command"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	ID     int64  `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// requestTimeout bounds one request, so that a stuck network cannot block
// the application's thread forever.
const requestTimeout = 30 * time.Second

// Call runs one JSON request and returns the JSON response. It never fails:
// errors are reported in the response.
func (c *Core) Call(data []byte) []byte {
	var req request
	resp := response{}
	if err := json.Unmarshal(data, &req); err != nil {
		resp.Error = "invalid request: " + err.Error()
	} else {
		resp.ID = req.ID
		ctx, cancel := context.WithTimeout(c.ctx, requestTimeout)
		result, err := c.dispatch(ctx, req)
		cancel()
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Result = result
		}
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return []byte(`{"error":"failed to encode the response"}`)
	}
	return out
}

func (c *Core) dispatch(ctx context.Context, req request) (any, error) {
	switch req.Command {
	case "ping":
		var p struct {
			Payload string `json:"payload"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return p, nil
	case "login":
		var p struct {
			Username string `json:"username"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		return c.login(ctx, p.Username)
	case "rooms":
		return c.rooms(ctx)
	case "send":
		var p struct {
			RoomID id.RoomID `json:"room_id"`
			Text   string    `json:"text"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, err
		}
		eventID, err := c.host.Matrix.SendMessage(ctx, p.RoomID, &event.MessageEventContent{MsgType: event.MsgText, Body: p.Text})
		if err != nil {
			return nil, err
		}
		return map[string]string{"event_id": string(eventID)}, nil
	case "stats":
		return stats(), nil
	default:
		return nil, fmt.Errorf("unknown command %q", req.Command)
	}
}

func decodeParams(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

type loginResult struct {
	LoginID string `json:"login_id"`
	Rooms   []room `json:"rooms"`
}

type room struct {
	RoomID id.RoomID `json:"room_id"`
	Name   string    `json:"name"`
}

// login runs the echo network's login flow and waits until the login is
// connected and its conversations exist.
func (c *Core) login(ctx context.Context, username string) (*loginResult, error) {
	br := c.host.Bridge(echoBridge)
	user, err := c.host.User(ctx, echoBridge)
	if err != nil {
		return nil, err
	}
	process, err := br.Network.CreateLogin(ctx, user, echo.FlowUsername)
	if err != nil {
		return nil, err
	}
	step, err := process.Start(ctx)
	if err != nil {
		return nil, err
	}
	if step.Type != bridgev2.LoginStepTypeUserInput || len(step.UserInputParams.Fields) != 1 {
		return nil, fmt.Errorf("unexpected login step %q", step.StepID)
	}
	field := step.UserInputParams.Fields[0].ID
	step, err = process.(bridgev2.LoginProcessUserInput).SubmitUserInput(ctx, map[string]string{field: username})
	if err != nil {
		return nil, err
	}
	if step.Type != bridgev2.LoginStepTypeComplete || step.CompleteParams.UserLogin == nil {
		return nil, fmt.Errorf("unexpected login step %q", step.StepID)
	}
	loginID := step.CompleteParams.UserLogin.ID
	for {
		state, err := c.host.Matrix.BridgeState(ctx, string(echoBridge), string(loginID))
		if err != nil {
			return nil, err
		}
		rooms, err := c.rooms(ctx)
		if err != nil {
			return nil, err
		}
		if state != nil && state.StateEvent == status.StateConnected && len(rooms) == len(echo.Contacts) {
			return &loginResult{LoginID: string(loginID), Rooms: rooms}, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for the login to connect: %w", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// rooms lists the rooms whose name is set, i.e. fully created.
func (c *Core) rooms(ctx context.Context) ([]room, error) {
	all, err := c.host.Matrix.Rooms(ctx)
	if err != nil {
		return nil, err
	}
	rooms := make([]room, 0, len(all))
	for _, r := range all {
		name, err := c.host.Matrix.State(ctx, r.ID, event.StateRoomName, "")
		if err != nil {
			return nil, err
		} else if name == nil {
			continue
		}
		rooms = append(rooms, room{RoomID: r.ID, Name: name.Content.AsRoomName().Name})
	}
	return rooms, nil
}

type statsResult struct {
	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
	HeapSysBytes   uint64 `json:"heap_sys_bytes"`
	Goroutines     int    `json:"goroutines"`
}

// stats reports the Go runtime's memory after a garbage collection, for the
// leak tests of the embedding application.
func stats() statsResult {
	runtime.GC()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return statsResult{HeapAllocBytes: mem.HeapAlloc, HeapSysBytes: mem.HeapSys, Goroutines: runtime.NumGoroutine()}
}

// logWriter writes the core's logs to its file. bridgev2 does not wait for
// the portal events it is handling when it stops: it cancels their context,
// and they log their failure ("context canceled") after Stop has returned
// (see docs/ADR/0010-core-shared-library.md). Such late writes are dropped
// rather than failing on a closed file, and kept for the tests, which check
// that nothing else outlives Close.
type logWriter struct {
	mu     sync.Mutex
	f      *os.File
	closed bool
	// late keeps the first lines written after close.
	late []string
}

const maxLateLines = 64

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		if len(w.late) < maxLateLines {
			w.late = append(w.late, string(p))
		}
		return len(p), nil
	}
	return w.f.Write(p)
}

func (w *logWriter) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return w.f.Close()
}

func (w *logWriter) lateLines() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.late...)
}
