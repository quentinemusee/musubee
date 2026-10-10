// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package memprobe

import (
	"encoding/json"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func stepNames(r Report) []string {
	names := make([]string, len(r.Steps))
	for i, m := range r.Steps {
		names[i] = m.Step
	}
	return names
}

// skipWithoutCore skips a test of the core steps in a memprobe_nocore build.
func skipWithoutCore(t *testing.T) {
	t.Helper()
	if sqliteDriver == "none" {
		t.Skip("the core is not built in (memprobe_nocore)")
	}
}

func TestRunEveryStep(t *testing.T) {
	skipWithoutCore(t)
	r := Run(Config{DataDir: t.TempDir(), Core: true, Crypto: true, Messages: 5}, OSFootprint)
	if r.Error != "" {
		t.Fatalf("Run failed: %s", r.Error)
	}
	want := []string{"go_runtime", "core_open", "echo_login", "echo_messages_5", "core_close", "olm_account", "olm_room_key", "megolm_decrypt_5", "gc_free"}
	if got := stepNames(r); !slices.Equal(got, want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
	for _, m := range r.Steps {
		if m.GoMappedBytes == 0 || m.GoHeapObjectsBytes == 0 || m.GoStackBytes == 0 || m.Goroutines == 0 || m.DurationMS < 0 {
			t.Errorf("%s: missing Go figures: %+v", m.Step, m)
		}
		known := runtime.GOOS == "linux" || runtime.GOOS == "android" || (runtime.GOOS == "darwin" && cgoEnabled)
		if known && (m.FootprintBytes <= 0 || m.PeakFootprintBytes < m.FootprintBytes) {
			t.Errorf("%s: footprint %d, peak %d", m.Step, m.FootprintBytes, m.PeakFootprintBytes)
		}
	}
	// The core's goroutines are gone once it is closed.
	if open, closed := r.Steps[3].Goroutines, r.Steps[4].Goroutines; closed >= open {
		t.Errorf("goroutines: %d with the core, %d after closing it", open, closed)
	}
	out, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s", out)
}

func TestRunOnlyCrypto(t *testing.T) {
	r := Run(Config{Crypto: true}, Unknown)
	want := []string{"go_runtime", "olm_account", "olm_room_key", "megolm_decrypt_20", "gc_free"}
	if got := stepNames(r); r.Error != "" || !slices.Equal(got, want) {
		t.Fatalf("steps = %v (error %q), want %v", got, r.Error, want)
	}
	if m := r.Steps[0]; m.FootprintBytes != -1 || m.PeakFootprintBytes != -1 || m.LimitRemainingBytes != -1 {
		t.Errorf("Unknown footprint reported as %+v", m)
	}
}

func TestRunStopsAtTheFirstFailure(t *testing.T) {
	skipWithoutCore(t)
	r := Run(Config{Core: true, Crypto: true}, Unknown)
	if got := stepNames(r); !slices.Equal(got, []string{"go_runtime", "core_open"}) {
		t.Fatalf("steps = %v", got)
	}
	if !strings.HasPrefix(r.Error, "core_open: ") || !strings.Contains(r.Error, "data_dir") {
		t.Errorf("error = %q", r.Error)
	}
}

func TestRunWithoutTheCore(t *testing.T) {
	if sqliteDriver != "none" {
		t.Skip("the core is built in")
	}
	r := Run(Config{Core: true, Crypto: true}, Unknown)
	if got := stepNames(r); !slices.Equal(got, []string{"go_runtime", "core_open"}) || !strings.Contains(r.Error, "memprobe_nocore") {
		t.Fatalf("steps = %v, error %q", got, r.Error)
	}
}
