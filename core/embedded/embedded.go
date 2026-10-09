// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package embedded is the core as an embedding application sees it: JSON
// requests in, JSON responses and events out, as defined by the core API
// contract (package api, docs/ADR/0012-core-api-contract.md). The C library
// (package ffi) and the JNI entry points are thin layers over it, so that
// everything except the native calls is tested in Go.
package embedded

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/bridgehost"
	"github.com/quentinemusee/musubee/core/connector/echo"
	"github.com/quentinemusee/musubee/core/connector/telegram"
	"github.com/quentinemusee/musubee/core/localmatrix"
)

// Version is the version of the core build, reported by core.hello. Release
// builds set it with -ldflags "-X .../core/embedded.Version=...".
var Version = "dev"

// The bridge IDs of the networks.
const (
	echoBridge     networkid.BridgeID = "echo"
	telegramBridge networkid.BridgeID = "telegram"
)

// eventQueueSize bounds the events waiting for the application. When the
// application stops reading, the oldest events are not kept forever: the
// queue reports resync.required and the application must re-read the state.
const eventQueueSize = 1024

// Config is the JSON configuration passed to Open.
type Config struct {
	// DataDir holds the database and the log file. Required.
	DataDir string `json:"data_dir"`
	// LogLevel is a zerolog level name ("info" if empty).
	LogLevel string `json:"log_level,omitempty"`
	// EchoDelayMS is the delay of the echo network's delayed contact.
	EchoDelayMS int `json:"echo_delay_ms,omitempty"`
	// Telegram enables the Telegram network. Without it, the core does not
	// offer Telegram.
	Telegram *TelegramConfig `json:"telegram,omitempty"`
}

// TelegramConfig identifies the application to Telegram
// (https://my.telegram.org). The embedding application gets it from its
// build or its environment, never from the repository, and never logs it.
type TelegramConfig struct {
	APIID   int    `json:"api_id"`
	APIHash string `json:"api_hash"`
}

// Core is one running core.
type Core struct {
	host     *bridgehost.Host
	networks []networkid.BridgeID
	logs     *logWriter
	log      zerolog.Logger

	// ctx is cancelled by Close; it bounds every request.
	ctx    context.Context
	cancel context.CancelFunc

	loginsMu sync.Mutex
	logins   map[string]*loginProcess

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

	c := &Core{logs: logs, log: log, events: make(chan []byte, eventQueueSize), logins: map[string]*loginProcess{}}
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
	c.networks = append(c.networks, echoBridge)
	if cfg.Telegram != nil {
		tg, err := telegram.New(telegram.Config{APIID: cfg.Telegram.APIID, APIHash: cfg.Telegram.APIHash})
		if err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}
		if _, err = host.AddNetwork(telegramBridge, tg); err != nil {
			return err
		}
		c.networks = append(c.networks, telegramBridge)
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
		c.cancelLogins()
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
// timeout, and a final core.closed event once the core is closed.
func (c *Core) NextEvent(timeout time.Duration) ([]byte, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case evt, ok := <-c.events:
		if !ok {
			return closedEvent, true
		}
		return evt, true
	case <-timer.C:
		return nil, false
	}
}

// forward turns the local storage's updates into API events.
func (c *Core) forward(updates <-chan localmatrix.Update) {
	defer c.forwarding.Done()
	for update := range updates {
		evt, err := c.translate(update)
		if err != nil {
			if c.ctx.Err() == nil {
				c.log.Err(err).Msg("Failed to translate an update for the application")
			}
			continue
		}
		if evt != nil {
			c.pushEvent(evt)
		}
	}
	// The channel closes when the core stops, or when this subscriber fell
	// behind and the server dropped it.
	if c.ctx.Err() == nil {
		c.push(resyncEvent)
		c.log.Warn().Msg("The application read events too slowly; it must re-read the state")
	}
}

// translate returns the API event of an update, or nil if it has none.
func (c *Core) translate(update localmatrix.Update) (*api.Event, error) {
	ctx := c.ctx
	switch {
	case update.Event != nil && update.Event.Type == event.EventMessage:
		msg, err := c.message(ctx, update.Event)
		if err != nil {
			return nil, err
		}
		return &api.Event{Type: api.EventMessageAdded, Data: api.MessageEvent{Message: msg}}, nil
	case update.Event != nil && update.Event.Type == event.StateRoomName:
		c.announceConversation(update.Event.RoomID)
		return nil, nil
	case update.MessageStatus != nil:
		evt, err := c.host.Matrix.Event(ctx, update.MessageStatus.RoomID, update.MessageStatus.EventID)
		if err != nil {
			return nil, err
		}
		msg, err := c.message(ctx, evt)
		if err != nil {
			return nil, err
		}
		return &api.Event{Type: api.EventMessageUpdated, Data: api.MessageEvent{Message: msg}}, nil
	case update.BridgeState != nil && update.BridgeState.RemoteID != "":
		network := networkid.BridgeID(update.BridgeState.BridgeID)
		br := c.host.Bridge(network)
		if br == nil {
			return nil, nil
		}
		login := update.BridgeState.RemoteID
		account, err := c.account(ctx, network, login, loginName(br, login))
		if err != nil {
			return nil, err
		}
		return &api.Event{Type: api.EventAccountUpdated, Data: api.AccountEvent{Account: account}}, nil
	}
	return nil, nil
}

// conversationWait bounds how long a named room may take to become a portal
// before its conversation.updated event is given up.
const conversationWait = 10 * time.Second

// announceConversation sends conversation.updated for a room whose name was
// set. bridgev2 names a room while creating it and records it as a portal
// just after, so the event may have to wait for the portal; it waits on its
// own goroutine, without holding up the other events. It is only called
// from forward, so the wait group is never at zero here.
func (c *Core) announceConversation(roomID id.RoomID) {
	c.forwarding.Add(1)
	go func() {
		defer c.forwarding.Done()
		deadline := time.Now().Add(conversationWait)
		for {
			room, err := c.host.Matrix.Room(c.ctx, roomID)
			var conv *api.Conversation
			if err == nil {
				conv, err = c.conversation(c.ctx, room)
			}
			if err != nil {
				if c.ctx.Err() == nil {
					c.log.Err(err).Stringer("room_id", roomID).Msg("Failed to read a conversation")
				}
				return
			}
			if conv != nil {
				c.pushEvent(&api.Event{Type: api.EventConversationUpdated, Data: api.ConversationEvent{Conversation: *conv}})
				return
			}
			if time.Now().After(deadline) {
				c.log.Warn().Stringer("room_id", roomID).Msg("A named room did not become a conversation")
				return
			}
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
}

func (c *Core) pushEvent(evt *api.Event) {
	data, err := json.Marshal(evt)
	if err != nil {
		c.log.Err(err).Str("event_type", evt.Type).Msg("Failed to encode an event for the application")
		return
	}
	c.push(data)
}

var (
	resyncEvent = mustJSON(&api.Event{Type: api.EventResyncRequired, Data: api.Empty{}})
	// closedEvent is the last event, returned by NextEvent once the core
	// is closed.
	closedEvent = mustJSON(&api.Event{Type: api.EventCoreClosed, Data: api.Empty{}})
)

// ClosedEvent returns the core.closed event, for the bindings to report a
// core that is closed or was never open.
func ClosedEvent() []byte {
	return slices.Clone(closedEvent)
}

// ErrorResponse returns the error response to a request that cannot reach a
// core, for the bindings. Its ID is the request's, when the request has one.
func ErrorResponse(request []byte, code api.ErrorCode, message string) []byte {
	var req struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(request, &req)
	return mustJSON(response{ID: req.ID, Error: &api.CoreError{Code: code, Message: message}})
}

func mustJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// push queues an event without ever blocking the core: when the queue is
// full, the oldest event is dropped and resync.required is queued instead.
func (c *Core) push(data []byte) {
	for {
		select {
		case c.events <- data:
			return
		default:
		}
		select {
		case <-c.events:
			data = resyncEvent
		default:
		}
	}
}

// requestTimeout bounds one request, so that a stuck network cannot block
// the application's thread forever. login.wait is the exception: it waits
// for the user, and login.cancel or Close ends it.
const requestTimeout = 30 * time.Second

type response struct {
	ID     int64          `json:"id"`
	Result any            `json:"result,omitempty"`
	Error  *api.CoreError `json:"error,omitempty"`
}

// Call runs one JSON request and returns the JSON response. It never fails:
// errors are reported in the response.
func (c *Core) Call(data []byte) []byte {
	var req struct {
		ID      int64           `json:"id"`
		Command string          `json:"command"`
		Params  json.RawMessage `json:"params"`
	}
	var resp response
	if err := json.Unmarshal(data, &req); err != nil {
		resp.Error = newError(api.ErrorCodeInvalidRequest, "invalid request: %v", err)
	} else if resp.ID = req.ID; req.Command == "" {
		resp.Error = newError(api.ErrorCodeInvalidRequest, "invalid request: command is missing")
	} else {
		ctx, cancel := c.ctx, context.CancelFunc(func() {})
		if req.Command != api.CommandLoginWait {
			ctx, cancel = context.WithTimeout(c.ctx, requestTimeout)
		}
		result, err := c.dispatch(ctx, req.Command, req.Params)
		cancel()
		if err != nil {
			resp.Error = c.coreError(err)
		} else {
			resp.Result = result
		}
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return mustJSON(response{ID: resp.ID, Error: newError(api.ErrorCodeInternal, "failed to encode the response")})
	}
	return out
}

func (c *Core) dispatch(ctx context.Context, command string, params json.RawMessage) (any, error) {
	switch command {
	case api.CommandCoreHello:
		return handle(ctx, params, c.hello)
	case api.CommandNetworksList:
		return handle(ctx, params, c.networksList)
	case api.CommandAccountsList:
		return handle(ctx, params, c.accountsList)
	case api.CommandAccountsLogout:
		return handle(ctx, params, c.accountsLogout)
	case api.CommandLoginStart:
		return handle(ctx, params, c.loginStart)
	case api.CommandLoginSubmit:
		return handle(ctx, params, c.loginSubmit)
	case api.CommandLoginWait:
		return handle(ctx, params, c.loginWait)
	case api.CommandLoginCancel:
		return handle(ctx, params, c.loginCancel)
	case api.CommandConversationsList:
		return handle(ctx, params, c.conversationsList)
	case api.CommandMessagesList:
		return handle(ctx, params, c.messagesList)
	case api.CommandMessagesSend:
		return handle(ctx, params, c.messagesSend)
	case api.CommandDebugPing:
		return handle(ctx, params, c.ping)
	case api.CommandDebugStats:
		return handle(ctx, params, c.stats)
	default:
		return nil, newError(api.ErrorCodeUnknownCommand, "unknown command %q", command)
	}
}

// handle decodes the params of a command and runs it. Unknown params are
// ignored: a newer user interface may send fields this core does not know.
func handle[P, R any](ctx context.Context, raw json.RawMessage, fn func(context.Context, P) (R, error)) (any, error) {
	var params P
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, newError(api.ErrorCodeInvalidParams, "invalid params: %v", err)
		}
	}
	return fn(ctx, params)
}

func newError(code api.ErrorCode, format string, args ...any) *api.CoreError {
	return &api.CoreError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// coreError gives an error its API code. Once the core is closing, every
// error is closed: closing ends what the requests were using (Close forgets
// the login processes, for one), so their own errors would be misleading.
func (c *Core) coreError(err error) *api.CoreError {
	var coreErr *api.CoreError
	switch {
	case c.ctx.Err() != nil:
		return newError(api.ErrorCodeClosed, "the core is closed")
	case errors.As(err, &coreErr):
		return coreErr
	case errors.Is(err, context.DeadlineExceeded):
		return newError(api.ErrorCodeTimeout, "%v", err)
	case errors.Is(err, localmatrix.ErrNotFound):
		return newError(api.ErrorCodeNotFound, "%v", err)
	default:
		return newError(api.ErrorCodeInternal, "%v", err)
	}
}

func (c *Core) hasNetwork(network networkid.BridgeID) bool {
	return slices.Contains(c.networks, network)
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
