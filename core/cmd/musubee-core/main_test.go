// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/quentinemusee/musubee/core/embedded"
)

// These tests build the command and run it as the desktop app does: a child
// process spoken to through its standard input and output.

const timeout = 30 * time.Second

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "musubee-core-test-")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "musubee-core")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err = build.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *bytes.Buffer
	exited chan error
}

// startCollecting starts the core and returns its output lines on a channel
// that the test reads itself.
func startCollecting(t *testing.T, args ...string) (*process, <-chan string) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	p := &process{cmd: cmd, stdin: stdin, stderr: &bytes.Buffer{}, exited: make(chan error, 1)}
	cmd.Stderr = p.stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 64)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
		p.exited <- cmd.Wait()
	}()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return p, lines
}

func next(t *testing.T, lines <-chan string) (string, bool) {
	t.Helper()
	select {
	case line, ok := <-lines:
		return line, ok
	case <-time.After(timeout):
		t.Fatal("no output within the timeout")
		return "", false
	}
}

func waitExit(t *testing.T, p *process) error {
	t.Helper()
	select {
	case err := <-p.exited:
		return err
	case <-time.After(timeout):
		t.Fatal("the core did not exit")
		return nil
	}
}

// The core answers on its standard output, and exits cleanly when its
// standard input ends, after the core.closed event.
func TestServesStdioAndExitsAtEndOfInput(t *testing.T) {
	dir := t.TempDir()
	p, lines := startCollecting(t, "-data", dir)
	if _, err := io.WriteString(p.stdin, `{"id":1,"command":"core.hello"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	line, _ := next(t, lines)
	if !strings.HasPrefix(line, `{"id":1,"result":{"api_version":"1.`) {
		t.Fatalf("response to core.hello: %s", line)
	}
	_ = p.stdin.Close()
	var last string
	for line, ok := next(t, lines); ok; line, ok = next(t, lines) {
		last = line
	}
	if last != string(embedded.ClosedEvent()) {
		t.Errorf("last line %q, want core.closed", last)
	}
	if err := waitExit(t, p); err != nil {
		t.Errorf("exit: %v; stderr: %s", err, p.stderr)
	}
	if p.stderr.Len() > 0 {
		t.Errorf("stderr is not empty: %s", p.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "core.log")); err != nil {
		t.Errorf("no core.log in the data directory: %v", err)
	}
}

func TestRequiresADataDirectory(t *testing.T) {
	out, err := exec.Command(binary).CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Fatalf("exit: %v, want status 2; output: %s", err, out)
	}
}

func TestReportsAnUnusableDataDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(binary, "-data", file).CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.HasPrefix(string(out), "musubee-core: ") {
		t.Fatalf("exit: %v, want status 1; output: %s", err, out)
	}
}

func TestExitsOnTermination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no SIGTERM; the app ends the input or kills the process")
	}
	p, lines := startCollecting(t, "-data", t.TempDir())
	if _, err := io.WriteString(p.stdin, `{"id":1,"command":"core.hello"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	next(t, lines)
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	go func() {
		for range lines {
		}
	}()
	if err := waitExit(t, p); err != nil {
		t.Errorf("exit after SIGTERM: %v; stderr: %s", err, p.stderr)
	}
}

func TestTelegramConfig(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(key string) string { return vars[key] }
	}
	if cfg, err := telegramConfig(env(nil)); cfg != nil || err != nil {
		t.Errorf("no variables: %+v, %v", cfg, err)
	}
	cfg, err := telegramConfig(env(map[string]string{"MUSUBEE_TG_API_ID": "42", "MUSUBEE_TG_API_HASH": "fake-hash"}))
	if err != nil || cfg == nil || cfg.APIID != 42 || cfg.APIHash != "fake-hash" {
		t.Errorf("both variables: %+v, %v", cfg, err)
	}
	for _, vars := range []map[string]string{
		{"MUSUBEE_TG_API_ID": "42"},
		{"MUSUBEE_TG_API_HASH": "fake-hash"},
		{"MUSUBEE_TG_API_ID": "forty-two", "MUSUBEE_TG_API_HASH": "fake-hash"},
		{"MUSUBEE_TG_API_ID": "-1", "MUSUBEE_TG_API_HASH": "fake-hash"},
	} {
		_, err := telegramConfig(env(vars))
		if err == nil {
			t.Errorf("%v: no error", vars)
		} else if strings.Contains(err.Error(), "fake-hash") || strings.Contains(err.Error(), "forty-two") {
			t.Errorf("the error quotes a value: %v", err)
		}
	}
}
