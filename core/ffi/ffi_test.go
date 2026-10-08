// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build cgo

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const mib = 1 << 20

// hostResult is the JSON line printed by testdata/host.c.
type hostResult struct {
	MemoryStart uint64 `json:"memory_start"`
	MemoryOpen  uint64 `json:"memory_open"`
	OpenNS      uint64 `json:"open_ns"`
	CloseNS     uint64 `json:"close_ns"`
	Ping        struct {
		Iterations   int    `json:"iterations"`
		NSPerCall    uint64 `json:"ns_per_call"`
		MemoryBefore uint64 `json:"memory_before"`
		MemoryAfter  uint64 `json:"memory_after"`
		HeapBefore   uint64 `json:"heap_before"`
		HeapAfter    uint64 `json:"heap_after"`
	} `json:"ping"`
	Roundtrip struct {
		Iterations      int    `json:"iterations"`
		MeanNS          uint64 `json:"mean_ns"`
		MaxNS           uint64 `json:"max_ns"`
		MemoryBefore    uint64 `json:"memory_before"`
		MemoryAfter     uint64 `json:"memory_after"`
		HeapAfter       uint64 `json:"heap_after"`
		GoroutinesAfter uint64 `json:"goroutines_after"`
	} `json:"roundtrip"`
}

func growth(before, after uint64) int64 { return int64(after) - int64(before) }

// cCompiler returns the C compiler cgo uses: with cgo enabled, there is one.
func cCompiler(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "CC").Output()
	if err != nil {
		t.Fatalf("go env CC: %v", err)
	}
	cc := strings.TrimSpace(string(out))
	path, err := exec.LookPath(cc)
	if err != nil {
		t.Fatalf("C compiler %q: %v", cc, err)
	}
	return path
}

// buildHost builds the core as a shared library and the C host program
// linked to it, in a temporary directory; it returns the host's path.
func buildHost(t *testing.T) string {
	t.Helper()
	cc := cCompiler(t)
	dir := t.TempDir()
	lib := map[string]string{"windows": "musubee.dll", "darwin": "libmusubee.dylib"}[runtime.GOOS]
	if lib == "" {
		lib = "libmusubee.so"
	}
	buildArgs := []string{"build", "-buildmode=c-shared", "-o", filepath.Join(dir, lib)}
	if runtime.GOOS == "darwin" {
		// Found through the host's rpath, like a library in an app bundle.
		buildArgs = append(buildArgs, "-ldflags=-extldflags=-Wl,-install_name,@rpath/"+lib)
	}
	build := exec.Command("go", append(buildArgs, ".")...)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the shared library: %v\n%s", err, out)
	}
	info, err := os.Stat(filepath.Join(dir, lib))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %.1f MiB", lib, float64(info.Size())/mib)

	host := filepath.Join(dir, "host")
	if runtime.GOOS == "windows" {
		host += ".exe"
	}
	// The include directory is this one, for musubee.h: never the output
	// directory, where cgo writes its own header with the same name.
	args := []string{"-O2", "-Wall", "-Wextra", "-Werror", "-I.", "-o", host, filepath.Join("testdata", "host.c")}
	switch runtime.GOOS {
	case "windows":
		// MinGW links directly against the DLL, which sits next to host.exe.
		args = append(args, filepath.Join(dir, lib), "-lpsapi")
	case "darwin":
		args = append(args, "-L"+dir, "-lmusubee", "-Wl,-rpath,@loader_path")
	default:
		args = append(args, "-L"+dir, "-lmusubee", "-Wl,-rpath,$ORIGIN")
	}
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("building the host program: %v\n%s", err, out)
	}
	return host
}

func iterations(name string, fallback int) string {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return strconv.Itoa(v)
	}
	return strconv.Itoa(fallback)
}

func runHost(t *testing.T, host string, extra ...string) hostResult {
	t.Helper()
	args := append([]string{t.TempDir(),
		iterations("MUSUBEE_FFI_PINGS", 100_000),
		iterations("MUSUBEE_FFI_ROUNDTRIPS", 200),
	}, extra...)
	cmd := exec.Command(host, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("host %v: %v\nstderr:\n%s", args, err, stderr.String())
	}
	var result hostResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("host output %q: %v", stdout.String(), err)
	}
	return result
}

// TestSharedLibrary drives the core from C through the shared library.
func TestSharedLibrary(t *testing.T) {
	host := buildHost(t)
	t.Run("RoundTrip", func(t *testing.T) { testRoundTrip(t, host) })
	t.Run("LeakIsDetected", func(t *testing.T) { testLeakIsDetected(t, host) })
}

// testRoundTrip checks requests, events and close, and memory over long
// loops.
func testRoundTrip(t *testing.T, host string) {
	r := runHost(t, host)
	t.Logf("open %.1f ms, close %.1f ms; memory %.1f MiB at start, %.1f MiB once open",
		float64(r.OpenNS)/1e6, float64(r.CloseNS)/1e6, float64(r.MemoryStart)/mib, float64(r.MemoryOpen)/mib)
	t.Logf("ping x%d (1 KiB): %.1f us/call; memory %+.1f MiB, Go heap %+.1f MiB",
		r.Ping.Iterations, float64(r.Ping.NSPerCall)/1e3,
		float64(growth(r.Ping.MemoryBefore, r.Ping.MemoryAfter))/mib, float64(growth(r.Ping.HeapBefore, r.Ping.HeapAfter))/mib)
	t.Logf("round trip x%d: mean %.2f ms, max %.2f ms; memory %+.1f MiB; Go heap %.1f MiB, %d goroutines",
		r.Roundtrip.Iterations, float64(r.Roundtrip.MeanNS)/1e6, float64(r.Roundtrip.MaxNS)/1e6,
		float64(growth(r.Roundtrip.MemoryBefore, r.Roundtrip.MemoryAfter))/mib, float64(r.Roundtrip.HeapAfter)/mib, r.Roundtrip.GoroutinesAfter)

	// A leak of the responses alone would be over 100 MiB for 100,000
	// pings (testLeakIsDetected): the bounds leave room for the allocator
	// and the Go heap reaching their steady state.
	if g := growth(r.Ping.MemoryBefore, r.Ping.MemoryAfter); g > 16*mib {
		t.Errorf("memory grew by %.1f MiB over the ping loop", float64(g)/mib)
	}
	if g := growth(r.Ping.HeapBefore, r.Ping.HeapAfter); g > 4*mib {
		t.Errorf("Go heap grew by %.1f MiB over the ping loop", float64(g)/mib)
	}
	if g := growth(r.Roundtrip.MemoryBefore, r.Roundtrip.MemoryAfter); g > 32*mib {
		t.Errorf("memory grew by %.1f MiB over the round-trip loop", float64(g)/mib)
	}
}

// testLeakIsDetected checks that the memory measurement of testRoundTrip
// has the power to see a leak: the host forgets musubee_free.
func testLeakIsDetected(t *testing.T, host string) {
	r := runHost(t, host, "--leak")
	g := growth(r.Ping.MemoryBefore, r.Ping.MemoryAfter)
	t.Logf("without musubee_free: memory %+.1f MiB over %d pings", float64(g)/mib, r.Ping.Iterations)
	if want := int64(r.Ping.Iterations) * 1024; g < want/2 {
		t.Errorf("memory grew by %.1f MiB only, want about %.1f MiB", float64(g)/mib, float64(want)/mib)
	}
}
