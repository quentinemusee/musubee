// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package telegram runs mautrix-telegram's network connector inside the
// core, "on device": the core talks to Telegram's servers itself, and no
// server of ours sees the messages (docs/ADR/0014-telegram-on-device.md).
package telegram

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.mau.fi/mautrix-telegram/pkg/connector"
	"gopkg.in/yaml.v3"
	"maunium.net/go/mautrix/bridgev2"
)

// Config holds what the core needs to connect to Telegram.
type Config struct {
	// APIID and APIHash identify the application to Telegram
	// (https://my.telegram.org). They are never stored in the repository.
	APIID   int
	APIHash string
}

// DeviceModel is how sessions of Musubee appear in Telegram's list of
// devices.
const DeviceModel = "Musubee"

// hiddenFlows are login flows of mautrix-telegram that the core does not
// offer: "manual" imports raw session credentials, which upstream marks
// "advanced, do not use".
var hiddenFlows = []string{connector.LoginFlowIDManual}

// ErrFlowNotOffered is returned by CreateLogin for a hidden flow.
var ErrFlowNotOffered = errors.New("this login flow is not offered by Musubee")

// Connector is mautrix-telegram's connector with the core's restrictions.
// Embedding the pointer keeps every optional bridgev2 interface it
// implements.
type Connector struct {
	*connector.TelegramConnector
}

var _ bridgev2.NetworkConnector = Connector{}

// New returns a connector configured for the core: mautrix-telegram's
// defaults, the application's credentials, and no animated sticker
// conversion, which needs programs (lottieconverter, ffmpeg) that devices
// do not have.
func New(cfg Config) (Connector, error) {
	tc := &connector.TelegramConnector{}
	if err := yaml.Unmarshal([]byte(connector.ExampleConfig), &tc.Config); err != nil {
		return Connector{}, fmt.Errorf("telegram: default configuration: %w", err)
	}
	tc.Config.APIID = cfg.APIID
	tc.Config.APIHash = cfg.APIHash
	tc.Config.DeviceInfo.DeviceModel = DeviceModel
	tc.Config.AnimatedSticker.Target = "disable"
	if err := tc.ValidateConfig(); err != nil {
		return Connector{}, fmt.Errorf("telegram: %w", err)
	}
	return Connector{tc}, nil
}

// GetLoginFlows returns mautrix-telegram's flows, without the hidden ones.
func (c Connector) GetLoginFlows() []bridgev2.LoginFlow {
	return slices.DeleteFunc(c.TelegramConnector.GetLoginFlows(), func(f bridgev2.LoginFlow) bool {
		return slices.Contains(hiddenFlows, f.ID)
	})
}

// CreateLogin refuses the hidden flows.
func (c Connector) CreateLogin(ctx context.Context, user *bridgev2.User, flowID string) (bridgev2.LoginProcess, error) {
	if slices.Contains(hiddenFlows, flowID) {
		return nil, ErrFlowNotOffered
	}
	return c.TelegramConnector.CreateLogin(ctx, user, flowID)
}
