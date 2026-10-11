// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !musubee_signal

package signal

import (
	"errors"
	"testing"
)

func TestNewWithoutSignal(t *testing.T) {
	if Available {
		t.Fatal("Available without the build tag")
	}
	if _, err := New(Config{}); !errors.Is(err, ErrNotBuilt) {
		t.Fatalf("New: %v, want ErrNotBuilt", err)
	}
}
