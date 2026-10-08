// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgehost_test

import (
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/event"

	"github.com/quentinemusee/musubee/core/connector/echo"
)

// BenchmarkRoundTrip measures a message going to the echo network and its
// echo coming back to the local storage, as seen by a subscriber (the UI).
// It also reports the live heap after the run, which includes the test
// harness's in-memory log buffer. Figures: docs/ADR/0009.
//
//	go test -run '^$' -bench RoundTrip -benchtime 2000x ./core/bridgehost/
func BenchmarkRoundTrip(b *testing.B) {
	// Info is the production log level: trace logs would dominate the figures.
	h := startHostWithLogLevel(b, zerolog.InfoLevel, filepath.Join(b.TempDir(), "core.db"), echoBridge)
	login := h.login(echoBridge, "alice")
	roomID := h.room(echoBridge, login.ID, echo.ContactInstant)
	ghost := h.host.Bridge(echoBridge).Matrix.FormatGhostMXID(echo.ContactInstant)
	updates, unsubscribe := h.host.Matrix.Subscribe(1024)
	defer unsubscribe()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sent := h.send(roomID, "message "+strconv.Itoa(i))
		timeout := time.After(waitTimeout)
	wait:
		for {
			select {
			case update, ok := <-updates:
				if !ok {
					b.Fatal("the benchmark was dropped as a slow subscriber")
				}
				if update.Event != nil && update.Event.Sender == ghost && update.Event.Type == event.EventMessage {
					break wait
				}
			case <-timeout:
				// A lost echo is a bug (see docs/ADR/0009 for the one found
				// this way): the status tells whether the bridge saw it.
				st, err := h.host.Matrix.MessageStatus(b.Context(), sent)
				b.Fatalf("no echo for message %d after %s; its status: %+v (%v)", i, waitTimeout, st, err)
			}
		}
	}
	b.StopTimer()

	runtime.GC()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	b.ReportMetric(float64(mem.HeapAlloc)/(1<<20), "heap-MiB")
}
