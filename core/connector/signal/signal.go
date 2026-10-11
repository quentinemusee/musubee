// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build musubee_signal

package signal

import (
	"fmt"
	"sync"

	"go.mau.fi/mautrix-signal/pkg/connector"
	"go.mau.fi/mautrix-signal/pkg/signalmeow"
	"gopkg.in/yaml.v3"
	"maunium.net/go/mautrix/bridgev2"
)

// Available reports whether this core was built with Signal.
const Available = true

var (
	environmentMu  sync.Mutex
	environmentSet bool
)

// New returns mautrix-signal's connector configured for the core: its
// defaults, the device name, no avatar for "Note to Self" (upstream's is an
// image on maunium.net's Matrix server, which the core does not reach), the
// chosen Signal service, and its store's secrets sealed with cfg.Key.
func New(cfg Config) (bridgev2.NetworkConnector, error) {
	if cfg.Key == nil {
		return nil, ErrNoKey
	}
	env := signalmeow.EnvironmentProduction
	if cfg.Environment != "" {
		env = signalmeow.Environment(cfg.Environment)
	}
	if err := setEnvironment(env); err != nil {
		return nil, err
	}
	sc := &connector.SignalConnector{}
	if err := yaml.Unmarshal([]byte(connector.ExampleConfig), &sc.Config); err != nil {
		return nil, fmt.Errorf("signal: default configuration: %w", err)
	}
	sc.Config.DeviceName = DeviceName
	sc.Config.NoteToSelfAvatar = ""
	return &sealedConnector{SignalConnector: sc, sealer: newStoreSealer(*cfg.Key)}, nil
}

// sealedConnector is mautrix-signal's connector with its store sealed. The
// embedded pointer keeps every optional interface of the connector.
type sealedConnector struct {
	*connector.SignalConnector
	sealer *storeSealer
}

// Init creates the store, then gives it the sealer, before Start opens it.
func (c *sealedConnector) Init(bridge *bridgev2.Bridge) {
	c.SignalConnector.Init(bridge)
	c.Store.SetSealer(c.sealer)
}

func setEnvironment(env signalmeow.Environment) error {
	environmentMu.Lock()
	defer environmentMu.Unlock()
	if environmentSet {
		if signalmeow.CurrentEnvironment() != env {
			return ErrEnvironmentChanged
		}
		return nil
	}
	if err := signalmeow.SetEnvironment(env); err != nil {
		return fmt.Errorf("signal: %w", err)
	}
	environmentSet = true
	return nil
}
