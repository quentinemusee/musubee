// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package stream_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/api/apitest"
	"github.com/quentinemusee/musubee/core/api/stream"
	"github.com/quentinemusee/musubee/core/embedded"
)

// These tests run the real core behind Serve, through in-memory pipes, and
// check every line it writes against the schema.

const timeout = 15 * time.Second

// client plays the desktop app's side of the stream.
type client struct {
	t         *testing.T
	schema    *apitest.Validator
	in        *io.PipeWriter
	lines     chan []byte
	served    chan error
	lastID    int64
	commands  map[int64]string
	responses map[int64][]byte
	events    [][]byte
}

func start(t *testing.T, cfg embedded.Config) *client {
	t.Helper()
	schema, err := apitest.New()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DataDir = t.TempDir()
	data, _ := json.Marshal(cfg)
	core, err := embedded.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := &client{
		t: t, schema: schema, in: inW,
		lines:     make(chan []byte, 1024),
		served:    make(chan error, 1),
		commands:  map[int64]string{},
		responses: map[int64][]byte{},
	}
	go func() {
		err := stream.Serve(core, embedded.ClosedEvent(), inR, outW)
		_ = outW.Close()
		c.served <- err
	}()
	go func() {
		defer close(c.lines)
		scanner := bufio.NewScanner(outR)
		scanner.Buffer(nil, stream.MaxLineSize)
		for scanner.Scan() {
			c.lines <- bytes.Clone(scanner.Bytes())
		}
	}()
	t.Cleanup(func() {
		_ = inW.Close()
		deadline := time.After(timeout)
		for {
			select {
			case _, ok := <-c.lines:
				if !ok {
					return
				}
			case <-deadline:
				t.Error("the stream did not end after its input closed")
				return
			}
		}
	})
	return c
}

func (c *client) write(line string) {
	c.t.Helper()
	if _, err := io.WriteString(c.in, line+"\n"); err != nil {
		c.t.Fatal(err)
	}
}

// send writes a request and returns its ID.
func (c *client) send(command string, params any) int64 {
	c.t.Helper()
	c.lastID++
	req := map[string]any{"id": c.lastID, "command": command}
	if params != nil {
		req["params"] = params
	}
	data, _ := json.Marshal(req)
	c.commands[c.lastID] = command
	c.write(string(data))
	return c.lastID
}

// next returns the next line, or false when the stream ended.
func (c *client) next() ([]byte, bool) {
	c.t.Helper()
	select {
	case line, ok := <-c.lines:
		if ok {
			c.file(line)
		}
		return line, ok
	case <-time.After(timeout):
		c.t.Fatal("no line within the timeout")
		return nil, false
	}
}

// read returns the next line; the stream must not have ended.
func (c *client) read() []byte {
	c.t.Helper()
	line, ok := c.next()
	if !ok {
		c.t.Fatal("the stream ended")
	}
	return line
}

// file checks a line against the schema and files it as a response or an
// event.
func (c *client) file(line []byte) {
	c.t.Helper()
	var doc struct {
		ID *int64 `json:"id"`
	}
	if err := json.Unmarshal(line, &doc); err != nil {
		c.t.Fatalf("invalid line %s: %v", line, err)
	}
	if doc.ID != nil {
		if err := c.schema.ValidateResponse(c.commands[*doc.ID], line); err != nil {
			c.t.Errorf("response %s: %v", line, err)
		}
		c.responses[*doc.ID] = line
		return
	}
	if err := c.schema.ValidateEvent(line); err != nil {
		c.t.Errorf("event %s: %v", line, err)
	}
	c.events = append(c.events, line)
}

func (c *client) response(id int64) []byte {
	c.t.Helper()
	for c.responses[id] == nil {
		c.read()
	}
	return c.responses[id]
}

// call sends a request and decodes its result into R.
func call[R any](c *client, command string, params any) R {
	c.t.Helper()
	var resp struct {
		Result R              `json:"result"`
		Error  *api.CoreError `json:"error"`
	}
	line := c.response(c.send(command, params))
	if err := json.Unmarshal(line, &resp); err != nil || resp.Error != nil {
		c.t.Fatalf("%s: %s (%v)", command, line, err)
	}
	return resp.Result
}

// event is an event with its data still encoded.
type event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// waitEvent reads until an event of the type has data matching, looking at
// the events already read first, and returns its data.
func waitEvent[D any](c *client, eventType string, match func(D) bool) D {
	c.t.Helper()
	for i := 0; ; i++ {
		for i >= len(c.events) {
			c.read()
		}
		var evt event
		var data D
		if json.Unmarshal(c.events[i], &evt) == nil && evt.Type == eventType && json.Unmarshal(evt.Data, &data) == nil && match(data) {
			return data
		}
	}
}

// finish closes the input and returns what Serve returned, after checking
// that the stream ends with core.closed.
func (c *client) finish() error {
	c.t.Helper()
	_ = c.in.Close()
	var last []byte
	for line, ok := c.next(); ok; line, ok = c.next() {
		last = line
	}
	if !bytes.Equal(last, embedded.ClosedEvent()) {
		c.t.Errorf("the last line is %s, want core.closed", last)
	}
	select {
	case err := <-c.served:
		return err
	case <-time.After(timeout):
		c.t.Fatal("Serve did not return")
		return nil
	}
}

func TestServeRunsTheCore(t *testing.T) {
	c := start(t, embedded.Config{EchoDelayMS: 50})
	hello := call[api.HelloResult](c, api.CommandCoreHello, nil)
	if hello.APIVersion != api.APIVersion {
		t.Fatalf("api_version = %q", hello.APIVersion)
	}
	step := call[api.LoginStep](c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "echo", FlowID: "username"})
	call[api.LoginStep](c, api.CommandLoginSubmit, api.LoginSubmitParams{
		ProcessID: step.ProcessID, Values: map[string]string{step.Fields[0].FieldID: "stream"},
	})
	conversation := waitEvent(c, api.EventConversationUpdated, func(e api.ConversationEvent) bool {
		return e.Conversation.Name == "Instant Echo"
	}).Conversation
	call[api.MessagesSendResult](c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conversation.ConversationID, Text: "over the stream"})
	waitEvent(c, api.EventMessageAdded, func(e api.MessageEvent) bool {
		return !e.Message.FromMe && strings.Contains(e.Message.Text, "over the stream")
	})
	if err := c.finish(); err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestServeSkipsBlankLinesAndAnswersBadOnes(t *testing.T) {
	c := start(t, embedded.Config{})
	c.write("")
	c.write("   ")
	c.write("not json")
	var resp struct {
		ID    int64          `json:"id"`
		Error *api.CoreError `json:"error"`
	}
	line := c.read()
	if err := json.Unmarshal(line, &resp); err != nil || resp.Error == nil || resp.Error.Code != api.ErrorCodeInvalidRequest {
		t.Fatalf("answer to a malformed line: %s", line)
	}
	call[api.HelloResult](c, api.CommandCoreHello, nil)
	if err := c.finish(); err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

// Requests run concurrently: a slow request does not hold back the others,
// and every response finds its request.
func TestServeRunsRequestsConcurrently(t *testing.T) {
	c := start(t, embedded.Config{EchoDelayMS: int(time.Hour / time.Millisecond)})
	step := call[api.LoginStep](c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "echo", FlowID: "code"})
	wait := c.send(api.CommandLoginWait, api.LoginProcessParams{ProcessID: step.ProcessID})
	pings := map[int64]string{}
	for i := range 50 {
		payload := fmt.Sprintf("ping %d", i)
		pings[c.send(api.CommandDebugPing, api.PingPayload{Payload: payload})] = payload
	}
	for id, payload := range pings {
		var resp struct {
			Result api.PingPayload `json:"result"`
		}
		if err := json.Unmarshal(c.response(id), &resp); err != nil || resp.Result.Payload != payload {
			t.Fatalf("response %d: %s", id, c.responses[id])
		}
	}
	if c.responses[wait] != nil {
		t.Fatalf("login.wait ended early: %s", c.responses[wait])
	}
	// Closing the input ends the login.wait that would otherwise wait for an
	// hour, and its response comes before core.closed.
	if err := c.finish(); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if !bytes.Contains(c.responses[wait], []byte(`"error"`)) {
		t.Fatalf("login.wait after the input closed: %s", c.responses[wait])
	}
}

func TestServeEndsOnATooLongLine(t *testing.T) {
	c := start(t, embedded.Config{})
	go func() {
		_, _ = c.in.Write(bytes.Repeat([]byte("x"), stream.MaxLineSize+1))
	}()
	select {
	case err := <-c.served:
		if !errors.Is(err, bufio.ErrTooLong) {
			t.Fatalf("Serve: %v, want %v", err, bufio.ErrTooLong)
		}
	case <-time.After(timeout):
		t.Fatal("Serve did not return")
	}
}
