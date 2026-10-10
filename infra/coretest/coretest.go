// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package coretest drives the core through its API in end-to-end tests, as a
// user interface does: commands, the event stream, and the check that no
// Matrix identifier reaches the API (CLAUDE.md §2).
package coretest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/api/apitest"
	"github.com/quentinemusee/musubee/core/embedded"
)

// Open opens a core, closed when the test ends.
func Open(t *testing.T, cfg embedded.Config) *embedded.Core {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	c, err := embedded.Open(raw)
	if err != nil {
		t.Fatalf("opening the core: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

var nextID atomic.Int64

// Call runs a command and decodes its result into out (if not nil); an
// error of the core fails the test.
func Call(t *testing.T, c *embedded.Core, command string, params, out any) {
	t.Helper()
	if coreErr := TryCall(t, c, command, params, out); coreErr != nil {
		t.Fatalf("%s: %s: %s", command, coreErr.Code, coreErr.Message)
	}
}

// TryCall runs a command and returns the core's error, if any. The error
// messages of the core never quote what was submitted.
func TryCall(t *testing.T, c *embedded.Core, command string, params, out any) *api.CoreError {
	t.Helper()
	req, err := json.Marshal(map[string]any{"id": nextID.Add(1), "command": command, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *api.CoreError  `json:"error"`
	}
	raw := c.Call(req)
	checkNoMatrixIDs(t, command, raw, req)
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("%s: %v", command, err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out != nil {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
	}
	return nil
}

// checkNoMatrixIDs fails if a response or an event shows a Matrix
// identifier (apitest.MatrixIDs). Only the identifier is reported: the
// document may hold message text.
func checkNoMatrixIDs(t *testing.T, what string, document, request []byte) {
	t.Helper()
	found, err := apitest.MatrixIDs(document, request)
	if err != nil {
		t.Errorf("%s: %v", what, err)
	}
	for _, v := range found {
		t.Errorf("%s: a Matrix identifier reaches the API: %q", what, v)
	}
}

// Event is an event of the core.
type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Decode decodes the event's data; it returns true, for use in conditions.
func (e Event) Decode(t *testing.T, out any) bool {
	t.Helper()
	if err := json.Unmarshal(e.Data, out); err != nil {
		t.Fatalf("event %s: %v", e.Type, err)
	}
	return true
}

// Events reads the core's events in the background, so that the core never
// drops them while the test waits on a network.
type Events chan Event

// Pump reads the events of a core until it closes. It checks each for
// Matrix identifiers, and reports them when the test ends.
func Pump(t *testing.T, c *embedded.Core) Events {
	ch := make(Events, 4096)
	var mu sync.Mutex
	var leaks []string
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, leak := range leaks {
			t.Errorf("a Matrix identifier reaches the API: %s", leak)
		}
	})
	go func() {
		defer close(ch)
		for {
			data, ok := c.NextEvent(time.Second)
			if !ok {
				continue
			}
			var e Event
			if json.Unmarshal(data, &e) != nil || e.Type == api.EventCoreClosed {
				return
			}
			found, _ := apitest.MatrixIDs(data, nil)
			mu.Lock()
			for _, v := range found {
				leaks = append(leaks, fmt.Sprintf("%q in a %s event", v, e.Type))
			}
			mu.Unlock()
			ch <- e
		}
	}()
	return ch
}

// Find returns the first event that matches within the delay, skipping the
// others.
func (ch Events) Find(ctx context.Context, within time.Duration, match func(Event) bool) (Event, bool) {
	timer := time.NewTimer(within)
	defer timer.Stop()
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return Event{}, false
			}
			if match(e) {
				return e, true
			}
		case <-timer.C:
			return Event{}, false
		case <-ctx.Done():
			return Event{}, false
		}
	}
}

// Wait is Find, failing the test when no event matches.
func (ch Events) Wait(t *testing.T, ctx context.Context, what string, within time.Duration, match func(Event) bool) Event {
	t.Helper()
	e, ok := ch.Find(ctx, within, match)
	if !ok {
		t.Fatalf("no event for %s within %s", what, within)
	}
	return e
}

// RandomHex returns 8 random hexadecimal digits, to make texts and names
// unique.
func RandomHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}
