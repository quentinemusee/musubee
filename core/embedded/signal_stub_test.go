// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !musubee_signal

package embedded

import (
	"encoding/json"
	"strings"
	"testing"
)

// A core built without Signal does not open when asked for Signal, rather
// than silently not offering it.
func TestSignalNeedsACoreBuiltWithIt(t *testing.T) {
	cfg, err := json.Marshal(Config{DataDir: t.TempDir(), Signal: &SignalConfig{}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Open(cfg)
	if err == nil {
		_ = c.Close()
		t.Fatal("Open succeeded")
	}
	if !strings.Contains(err.Error(), "built without Signal") {
		t.Errorf("Open: %v", err)
	}
}
