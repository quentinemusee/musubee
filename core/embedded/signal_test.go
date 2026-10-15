// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build musubee_signal

package embedded

import (
	"encoding/json"
	"testing"

	signalconnector "go.mau.fi/mautrix-signal/pkg/connector"

	"github.com/quentinemusee/musubee/core/api"
)

// With Signal in the configuration, a core built with it offers Signal and
// its QR code login. Nothing connects to Signal before a login.
func TestSignalIsOffered(t *testing.T) {
	cfg, err := json.Marshal(Config{DataDir: t.TempDir(), Signal: &SignalConfig{Environment: "staging"}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	networks := call[api.NetworksListResult](t, c, api.CommandNetworksList, nil).Networks
	if len(networks) != 3 || networks[2].NetworkID != "signal" || networks[2].Name != "Signal" {
		t.Fatalf("networks = %+v", networks)
	}
	if flows := networks[2].LoginFlows; len(flows) != 1 || flows[0].FlowID != "qr" || flows[0].Name != "By linking this device" {
		t.Errorf("signal flows = %+v", flows)
	}
	if networks[2].Description == "" {
		t.Error("signal has no description")
	}
	// The wording of the QR code step follows mautrix-signal's step ID.
	if _, ok := wordings[signalBridge].steps[signalconnector.LoginStepQR]; !ok {
		t.Errorf("no wording for mautrix-signal's QR code step %q", signalconnector.LoginStepQR)
	}
}
