// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !musubee_signal

package signal

import "maunium.net/go/mautrix/bridgev2"

// Available reports whether this core was built with Signal.
const Available = false

// New always fails: this core was built without Signal.
func New(Config) (bridgev2.NetworkConnector, error) {
	return nil, ErrNotBuilt
}
