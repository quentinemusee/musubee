// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !memprobe_nocore

package memprobe

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/embedded"
	"github.com/quentinemusee/musubee/core/storage/sqlite"
)

// The core steps. The memprobe_nocore build tag leaves them out, and with
// them every package of the core: the probe then measures the Go runtime
// with goolm alone, as a dedicated extension binary would be.

const sqliteDriver = sqlite.Driver

// stepTimeout bounds every wait for the core. An NSE has about 30 seconds in
// all.
const stepTimeout = 10 * time.Second

// coreSteps opens a core, logs in to echo, exchanges messages and closes
// the core. It returns whether every step succeeded.
func (p *prober) coreSteps(cfg Config) bool {
	var c *embedded.Core
	ok := p.step("core_open", func() (err error) {
		c, err = openCore(cfg.DataDir)
		return err
	})
	var conv string
	ok = ok && p.step("echo_login", func() (err error) {
		conv, err = loginEcho(c)
		return err
	})
	ok = ok && p.step(fmt.Sprintf("echo_messages_%d", cfg.Messages), func() error {
		return exchangeMessages(c, conv, cfg.Messages)
	})
	if c != nil {
		p.step("core_close", c.Close)
	}
	return ok
}

// openCore opens a core on an empty data directory, so that every run
// starts from the same state.
func openCore(dir string) (*embedded.Core, error) {
	if dir == "" {
		return nil, errors.New("data_dir is required for the core steps")
	}
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	cfg, err := json.Marshal(embedded.Config{DataDir: filepath.Clean(dir), LogLevel: "warn"})
	if err != nil {
		return nil, err
	}
	return embedded.Open(cfg)
}

// call runs one request and decodes its result into result (if not nil).
func call(c *embedded.Core, command string, params, result any) error {
	req, err := json.Marshal(map[string]any{"id": 1, "command": command, "params": params})
	if err != nil {
		return err
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *api.CoreError  `json:"error"`
	}
	if err = json.Unmarshal(c.Call(req), &resp); err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("%s: %s: %s", command, resp.Error.Code, resp.Error.Message)
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(resp.Result, result)
}

// loginEcho adds an echo account and returns the ID of its Instant Echo
// conversation, once it exists.
func loginEcho(c *embedded.Core) (string, error) {
	var step api.LoginStep
	if err := call(c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "echo", FlowID: "username"}, &step); err != nil {
		return "", err
	}
	if len(step.Fields) == 0 {
		return "", fmt.Errorf("unexpected login step %q", step.Type)
	}
	values := map[string]string{step.Fields[0].FieldID: "probe"}
	if err := call(c, api.CommandLoginSubmit, api.LoginSubmitParams{ProcessID: step.ProcessID, Values: values}, &step); err != nil {
		return "", err
	}
	if step.Type != api.LoginStepTypeComplete {
		return "", fmt.Errorf("login ended with step %q", step.Type)
	}
	deadline := time.Now().Add(stepTimeout)
	for time.Now().Before(deadline) {
		var list api.ConversationsListResult
		if err := call(c, api.CommandConversationsList, nil, &list); err != nil {
			return "", err
		}
		for _, conv := range list.Conversations {
			if conv.Name == "Instant Echo" {
				return conv.ConversationID, nil
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return "", errors.New("no Instant Echo conversation")
}

// exchangeMessages sends n messages and waits for the echo of each one.
func exchangeMessages(c *embedded.Core, conv string, n int) error {
	for i := range n {
		marker := fmt.Sprintf("probe-%d", i)
		if err := call(c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: conv, Text: marker}, nil); err != nil {
			return err
		}
		if err := waitEcho(c, marker); err != nil {
			return err
		}
	}
	return nil
}

func waitEcho(c *embedded.Core, marker string) error {
	deadline := time.Now().Add(stepTimeout)
	for {
		data, ok := c.NextEvent(time.Until(deadline))
		if !ok {
			return fmt.Errorf("no echo of %s within %s", marker, stepTimeout)
		}
		var evt struct {
			Type string           `json:"type"`
			Data api.MessageEvent `json:"data"`
		}
		if json.Unmarshal(data, &evt) != nil || evt.Type != api.EventMessageAdded {
			continue
		}
		if m := evt.Data.Message; !m.FromMe && strings.Contains(m.Text, marker) {
			return nil
		}
	}
}
