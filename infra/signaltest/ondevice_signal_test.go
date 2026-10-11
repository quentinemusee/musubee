// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build signal && musubee_signal

// The end-to-end test of Signal "on device" (T2.6, ADR 0025): the core, with
// mautrix-signal inside, talks to Signal's STAGING service itself. signal-cli
// plays the user's phone: it is the primary device of the test account
// (registered by the maintainer with register-staging.ps1), and links the
// core as a secondary device, as the Signal app does when the user scans the
// QR code. With one account, messages go through "Note to Self". Run it with:
//
//	go test -tags=goolm,signal,musubee_signal -v ./infra/signaltest/
//
// plus the libsignal link flags (scripts/build-libsignal.sh). It needs:
//
//	MUSUBEE_SIGNAL_CLI       signal-cli's installation directory (0.14.9+)
//	MUSUBEE_SIGNAL_CLI_DATA  signal-cli's state of the test account
//	MUSUBEE_SIGNAL_NUMBER    the test account's number
//	JAVA_HOME                a Java 25 (or later) runtime
//
// Without them it is skipped, unless MUSUBEE_REQUIRE_SIGNAL_E2E=1, in which
// case it fails. The account has one primary device: never run two of these
// tests at once.

package signaltest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/embedded"
	"github.com/quentinemusee/musubee/infra/coretest"
)

const (
	cliEnv    = "MUSUBEE_SIGNAL_CLI"
	dataEnv   = "MUSUBEE_SIGNAL_CLI_DATA"
	numberEnv = "MUSUBEE_SIGNAL_NUMBER"
)

// signalCLI runs signal-cli against the test account on staging. It calls
// Java directly rather than signal-cli's launcher: on Windows the launcher
// is a batch file, and cmd.exe would split the linking URI at its "&".
type signalCLI struct {
	java, classpath, data, number string
}

func newSignalCLI(t *testing.T) signalCLI {
	t.Helper()
	dir, data, number := os.Getenv(cliEnv), os.Getenv(dataEnv), os.Getenv(numberEnv)
	if dir == "" || data == "" || number == "" || os.Getenv("JAVA_HOME") == "" {
		if os.Getenv("MUSUBEE_REQUIRE_SIGNAL_E2E") == "1" {
			t.Fatalf("%s, %s, %s and JAVA_HOME are required", cliEnv, dataEnv, numberEnv)
		}
		t.Skipf("set %s, %s, %s and JAVA_HOME to run this test (see infra/signaltest/README.md)", cliEnv, dataEnv, numberEnv)
	}
	return signalCLI{
		java:      filepath.Join(os.Getenv("JAVA_HOME"), "bin", "java"),
		classpath: filepath.Join(dir, "lib", "*"),
		data:      data,
		number:    number,
	}
}

// run runs a signal-cli command and returns its standard output. Its output
// may hold message text: it is only shown when the command fails.
func (s signalCLI) run(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"--enable-native-access=ALL-UNNAMED", "-cp", s.classpath, "org.asamk.signal.Main",
		"--config", s.data, "--service-environment", "staging", "-a", s.number, "-o", "json"}, args...)
	cmd := exec.CommandContext(ctx, s.java, full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("signal-cli %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// device is one entry of signal-cli's listDevices.
type device struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (s signalCLI) devices(ctx context.Context, t *testing.T) []device {
	t.Helper()
	out, err := s.run(ctx, "listDevices")
	if err != nil {
		t.Fatal(err)
	}
	var devices []device
	if err := json.Unmarshal([]byte(out), &devices); err != nil {
		t.Fatalf("listDevices: %v", err)
	}
	return devices
}

// removeLinkedDevices unlinks every device but the primary (ID 1), so that
// a failed run leaves nothing behind for the next one.
func (s signalCLI) removeLinkedDevices(ctx context.Context, t *testing.T) {
	t.Helper()
	for _, d := range s.devices(ctx, t) {
		if d.ID != 1 {
			if _, err := s.run(ctx, "removeDevice", "--device-id", fmt.Sprint(d.ID)); err != nil {
				t.Errorf("unlinking device %d: %v", d.ID, err)
			}
		}
	}
}

// receivedText reports whether signal-cli's receive output (one JSON object
// per line) holds a message with this text, sent from another device of the
// account to Note to Self (a sync message of a sent message).
func receivedText(out, text string) bool {
	for _, line := range strings.Split(out, "\n") {
		var e struct {
			Envelope struct {
				SyncMessage *struct {
					SentMessage *struct {
						Message string `json:"message"`
					} `json:"sentMessage"`
				} `json:"syncMessage"`
				DataMessage *struct {
					Message string `json:"message"`
				} `json:"dataMessage"`
			} `json:"envelope"`
		}
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if sm := e.Envelope.SyncMessage; sm != nil && sm.SentMessage != nil && sm.SentMessage.Message == text {
			return true
		}
		if dm := e.Envelope.DataMessage; dm != nil && dm.Message == text {
			return true
		}
	}
	return false
}

var linkURI = regexp.MustCompile(`^sgnl://linkdevice\?`)

// TestSignalOnDevice links the core to the test account, sends a Note to
// Self from the "phone" and checks the core receives it, answers from the
// core and checks the phone sees it, then unlinks the core.
func TestSignalOnDevice(t *testing.T) {
	cli := newSignalCLI(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	// Leftovers of an earlier run would make the account's device list grow.
	cli.removeLinkedDevices(ctx, t)
	// Drain what is waiting for the primary device, so that later receives
	// only see this run's messages.
	if _, err := cli.run(ctx, "receive", "--timeout", "5"); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	started := time.Now()
	c := coretest.Open(t, embedded.Config{DataDir: dataDir, LogLevel: "debug", Signal: &embedded.SignalConfig{Environment: "staging"}})
	events := coretest.Pump(t, c)

	// 1. Log in with the QR code: the "phone" links the core.
	var step api.LoginStep
	coretest.Call(t, c, api.CommandLoginStart, api.LoginStartParams{NetworkID: "signal", FlowID: "qr"}, &step)
	if step.Type != api.LoginStepTypeDisplayAndWait || step.Display == nil || step.Display.Type != api.LoginDisplayTypeQR || !linkURI.MatchString(step.Display.Data) {
		t.Fatalf("QR login started with step %q, want a linking QR code", step.Type)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cli.removeLinkedDevices(cleanup, t)
	})
	linked := make(chan error, 1)
	go func() {
		_, err := cli.run(ctx, "addDevice", "--uri", step.Display.Data)
		linked <- err
	}()
	for step.Type == api.LoginStepTypeDisplayAndWait {
		coretest.Call(t, c, api.CommandLoginWait, api.LoginProcessParams{ProcessID: step.ProcessID}, &step)
	}
	if err := <-linked; err != nil {
		t.Fatal(err)
	}
	if step.Type != api.LoginStepTypeComplete || step.AccountID == "" {
		t.Fatalf("QR login ended with step %q", step.Type)
	}
	account := step.AccountID
	t.Logf("linked in %s", time.Since(started).Round(time.Millisecond))
	events.Wait(t, ctx, "the account connected", 2*time.Minute, func(e coretest.Event) bool {
		var data api.AccountEvent
		return e.Type == api.EventAccountUpdated && e.Decode(t, &data) &&
			data.Account.AccountID == account && data.Account.State == api.AccountStateConnected
	})
	var names []string
	for _, d := range cli.devices(ctx, t) {
		names = append(names, d.Name)
	}
	t.Logf("the account's devices: %q", names)

	// 2. phone -> core: a Note to Self from the primary device reaches the
	// core as a message of the Note to Self conversation, from the user.
	inbound := "musubee signal e2e: from the phone " + coretest.RandomHex(t)
	sent := time.Now()
	if _, err := cli.run(ctx, "send", "--note-to-self", "-m", inbound); err != nil {
		t.Fatal(err)
	}
	e := events.Wait(t, ctx, "the Note to Self in the core", 2*time.Minute, func(e coretest.Event) bool {
		var data api.MessageEvent
		return e.Type == api.EventMessageAdded && e.Decode(t, &data) && data.Message.Text == inbound
	})
	var received api.MessageEvent
	e.Decode(t, &received)
	t.Logf("phone -> core: %s", time.Since(sent).Round(time.Millisecond))
	if !received.Message.FromMe {
		t.Errorf("the Note to Self is not from the user")
	}

	// 3. core -> phone: an answer in the same conversation reaches the
	// primary device as a sync message.
	outbound := "musubee signal e2e: from the core " + coretest.RandomHex(t)
	sent = time.Now()
	var sendResult api.MessagesSendResult
	coretest.Call(t, c, api.CommandMessagesSend, api.MessagesSendParams{ConversationID: received.Message.ConversationID, Text: outbound}, &sendResult)
	events.Wait(t, ctx, "the answer sent", time.Minute, func(e coretest.Event) bool {
		var data api.MessageEvent
		return (e.Type == api.EventMessageUpdated || e.Type == api.EventMessageAdded) && e.Decode(t, &data) &&
			data.Message.MessageID == sendResult.Message.MessageID && data.Message.Status == api.MessageStatusSent
	})
	seen := false
	for deadline := time.Now().Add(2 * time.Minute); !seen && time.Now().Before(deadline); {
		out, err := cli.run(ctx, "receive", "--timeout", "10")
		if err != nil {
			t.Fatal(err)
		}
		seen = receivedText(out, outbound)
	}
	if !seen {
		t.Fatal("the phone did not receive the core's message within 2 minutes")
	}
	t.Logf("core -> phone: %s", time.Since(sent).Round(time.Millisecond))

	var stats api.StatsResult
	coretest.Call(t, c, api.CommandDebugStats, nil, &stats)
	t.Logf("core memory: heap %.1f MiB in use, %.1f MiB from the system; %d goroutines",
		float64(stats.HeapAllocBytes)/(1<<20), float64(stats.HeapSysBytes)/(1<<20), stats.Goroutines)

	// 4. Log out: the core unlinks itself.
	coretest.Call(t, c, api.CommandAccountsLogout, api.AccountsLogoutParams{AccountID: account}, nil)
	if err := c.Close(); err != nil {
		t.Errorf("closing the core: %v", err)
	}

	// 5. The log holds neither message.
	logData, err := os.ReadFile(filepath.Join(dataDir, "core.log"))
	if err != nil {
		t.Fatal(err)
	}
	for what, text := range map[string]string{"the received message": inbound, "the sent message": outbound} {
		if strings.Contains(string(logData), text) {
			t.Errorf("core.log contains %s", what)
		}
	}
}
