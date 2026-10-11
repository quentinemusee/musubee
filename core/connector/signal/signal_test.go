// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build musubee_signal

package signal

import (
	"errors"
	"testing"

	"go.mau.fi/mautrix-signal/pkg/connector"
	"go.mau.fi/mautrix-signal/pkg/signalmeow"
)

// The tests of this package share the process-wide environment, so they run
// in this order in one test.
func TestNew(t *testing.T) {
	if !Available {
		t.Fatal("not Available with the build tag")
	}
	c, err := New(Config{Environment: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	if signalmeow.CurrentEnvironment() != signalmeow.EnvironmentStaging {
		t.Errorf("environment %s, want staging", signalmeow.CurrentEnvironment())
	}
	cfg := c.(*connector.SignalConnector).Config
	if cfg.DeviceName != DeviceName {
		t.Errorf("device name %q, want %q", cfg.DeviceName, DeviceName)
	}
	if cfg.NoteToSelfAvatar != "" {
		t.Errorf("Note to Self avatar %q, want none", cfg.NoteToSelfAvatar)
	}
	// The upstream defaults are loaded, including the display name template.
	if cfg.DisplaynameTemplate == "" || cfg.LocationFormat == "" {
		t.Errorf("upstream defaults missing: %+v", cfg)
	}
	if flows := c.GetLoginFlows(); len(flows) != 1 || flows[0].ID != "qr" {
		t.Errorf("login flows %+v, want the QR code only", flows)
	}

	// The same environment again is fine, another one is refused.
	if _, err = New(Config{Environment: "staging"}); err != nil {
		t.Errorf("same environment refused: %v", err)
	}
	if _, err = New(Config{}); !errors.Is(err, ErrEnvironmentChanged) {
		t.Errorf("production after staging: %v, want ErrEnvironmentChanged", err)
	}
}
