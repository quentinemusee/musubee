// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgehost_test

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"maunium.net/go/mautrix/event"

	"github.com/quentinemusee/musubee/core/connector/echo"
)

// TestStopReleasesGoroutines starts and stops the core several times in one
// process, as the app does: no goroutine may survive a stop.
func TestStopReleasesGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	for range 3 {
		h := startHost(t, filepath.Join(t.TempDir(), "core.db"), echoBridge)
		login := h.login(echoBridge, "alice")
		roomID := h.room(echoBridge, login.ID, echo.ContactInstant)
		h.waitForStatus(h.send(roomID, "hello"), event.MessageStatusSuccess)
		h.stop()
	}
	deadline := time.Now().Add(waitTimeout)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		buf := make([]byte, 1<<20)
		t.Fatalf("%d goroutines before, %d after three start/stop cycles:\n%s", before, after, buf[:runtime.Stack(buf, true)])
	}
}
