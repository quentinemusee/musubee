// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package memprobe measures the memory that the core and Matrix encryption
// cost in the process that runs them, step by step. It exists for T1.7: the
// iOS Notification Service Extension (NSE) is a separate process with a small
// memory limit, and the question is whether a Go runtime with the core, or at
// least with goolm, fits in it (docs/ADR/0015-ios-nse-memory.md).
//
// Run performs the steps in order and measures after each one. The process
// footprint comes from the operating system (Footprint: on Apple platforms,
// the phys_footprint that jetsam compares with the limit); the Go runtime's
// own figures come from runtime/metrics, on every platform.
//
// The steps use the core through its JSON API, as an app would through the
// C library, and goolm (mautrix-go's pure-Go Olm and Megolm) the way an NSE
// would: receive a room key over Olm, then decrypt Megolm messages. The
// messages are generated test text; no message content is logged.
package memprobe

import (
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"strings"
	"time"

	"maunium.net/go/mautrix/crypto/goolm/account"
	"maunium.net/go/mautrix/crypto/goolm/session"
	"maunium.net/go/mautrix/id"
)

// Config selects what Run does. Its JSON form is the argument of the C
// function musubee_memprobe_run (core/cmd/memprobe).
type Config struct {
	// DataDir receives the core's database and log. Required unless Core is
	// false. Run empties it first.
	DataDir string `json:"data_dir"`
	// Core runs the core steps: open, echo login, messages, close.
	Core bool `json:"core"`
	// Crypto runs the goolm steps.
	Crypto bool `json:"crypto"`
	// Messages is the number of echo round trips and of Megolm messages
	// decrypted (20 if zero).
	Messages int `json:"messages,omitempty"`
	// MemoryLimitMB sets the Go runtime's soft memory limit
	// (debug.SetMemoryLimit, like GOMEMLIMIT) before the first step; 0 keeps
	// the current limit.
	MemoryLimitMB int `json:"memory_limit_mb,omitempty"`
	// GCPercent sets debug.SetGCPercent (like GOGC) before the first step;
	// 0 keeps the current value.
	GCPercent int `json:"gc_percent,omitempty"`
}

// Measure is the memory after one step. Byte counts of -1 are unknown on
// this platform.
type Measure struct {
	Step string `json:"step"`
	// DurationMS is the time the step took.
	DurationMS float64 `json:"duration_ms"`
	// FootprintBytes is the process's physical footprint (Apple:
	// phys_footprint, what the memory limit applies to).
	FootprintBytes int64 `json:"footprint_bytes"`
	// PeakFootprintBytes is the highest footprint of the process so far.
	PeakFootprintBytes int64 `json:"peak_footprint_bytes"`
	// LimitRemainingBytes is what is left before the operating system's
	// memory limit (Apple: limit_bytes_remaining; 0 when no limit applies,
	// as in the simulator).
	LimitRemainingBytes int64 `json:"limit_remaining_bytes"`
	// GoMappedBytes is all the memory the Go runtime has mapped, minus what
	// it has released to the operating system.
	GoMappedBytes uint64 `json:"go_mapped_bytes"`
	// GoHeapObjectsBytes is the memory of live and not yet swept heap objects.
	GoHeapObjectsBytes uint64 `json:"go_heap_objects_bytes"`
	// GoStackBytes is the memory of goroutine stacks.
	GoStackBytes uint64 `json:"go_stack_bytes"`
	Goroutines   int    `json:"goroutines"`
}

// Report is the result of Run.
type Report struct {
	GoVersion  string `json:"go_version"`
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	GOMAXPROCS int    `json:"gomaxprocs"`
	// SQLiteDriver is the SQLite build compiled in (sqlite.Driver), or
	// "none" without the core.
	SQLiteDriver string    `json:"sqlite_driver"`
	Config       Config    `json:"config"`
	Steps        []Measure `json:"steps"`
	// Error is the reason Run stopped early, if it did.
	Error string `json:"error,omitempty"`
}

// Footprint reads the process's memory figures from the operating system:
// its physical footprint, its peak footprint, and the bytes left before its
// limit. A figure is -1 when the platform does not provide it.
type Footprint func() (current, peak, limitRemaining int64)

// Unknown is a Footprint for platforms without one.
func Unknown() (current, peak, limitRemaining int64) { return -1, -1, -1 }

// Run performs the steps that cfg selects and measures after each one. It
// stops at the first failing step and reports its error. A step named
// go_runtime comes first: the memory of the process with the Go runtime
// started and nothing done yet.
func Run(cfg Config, footprint Footprint) Report {
	if cfg.Messages <= 0 {
		cfg.Messages = 20
	}
	if cfg.MemoryLimitMB > 0 {
		debug.SetMemoryLimit(int64(cfg.MemoryLimitMB) << 20)
	}
	if cfg.GCPercent != 0 {
		debug.SetGCPercent(cfg.GCPercent)
	}
	r := Report{
		GoVersion:    runtime.Version(),
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
		GOMAXPROCS:   runtime.GOMAXPROCS(0),
		SQLiteDriver: sqliteDriver,
		Config:       cfg,
	}
	p := &prober{footprint: footprint, report: &r}
	p.step("go_runtime", func() error { return nil })

	if cfg.Core && !p.coreSteps(cfg) {
		return r
	}

	if cfg.Crypto {
		var alice *account.Account
		ok := p.step("olm_account", func() (err error) {
			alice, err = newAccount()
			return err
		})
		var sender *session.MegolmOutboundSession
		var roomKey []byte
		ok = ok && p.step("olm_room_key", func() (err error) {
			sender, roomKey, err = receiveRoomKey(alice)
			return err
		})
		ok = ok && p.step(fmt.Sprintf("megolm_decrypt_%d", cfg.Messages), func() error {
			return decryptMessages(sender, roomKey, cfg.Messages)
		})
		if !ok {
			return r
		}
	}

	p.step("gc_free", func() error {
		debug.FreeOSMemory()
		return nil
	})
	return r
}

type prober struct {
	footprint Footprint
	report    *Report
	failed    bool
}

// step runs fn and records the memory after it. It returns whether fn
// succeeded; after a failure, it records the error and later steps do not
// run.
func (p *prober) step(name string, fn func() error) bool {
	if p.failed {
		return false
	}
	start := time.Now()
	err := fn()
	m := measure(p.footprint)
	m.Step = name
	m.DurationMS = float64(time.Since(start).Microseconds()) / 1000
	p.report.Steps = append(p.report.Steps, m)
	if err != nil {
		p.failed = true
		p.report.Error = fmt.Sprintf("%s: %v", name, err)
		return false
	}
	return true
}

var metricNames = []string{
	"/memory/classes/total:bytes",
	"/memory/classes/heap/released:bytes",
	"/memory/classes/heap/objects:bytes",
	"/memory/classes/heap/stacks:bytes",
}

func measure(footprint Footprint) Measure {
	samples := make([]metrics.Sample, len(metricNames))
	for i, name := range metricNames {
		samples[i].Name = name
	}
	metrics.Read(samples)
	value := func(i int) uint64 {
		if samples[i].Value.Kind() != metrics.KindUint64 {
			return 0
		}
		return samples[i].Value.Uint64()
	}
	var m Measure
	m.FootprintBytes, m.PeakFootprintBytes, m.LimitRemainingBytes = footprint()
	m.GoMappedBytes = value(0) - value(1)
	m.GoHeapObjectsBytes = value(2)
	m.GoStackBytes = value(3)
	m.Goroutines = runtime.NumGoroutine()
	return m
}

// newAccount creates an Olm account with one-time keys, as a device does
// once; an NSE would load it from storage instead, which costs less.
func newAccount() (*account.Account, error) {
	a, err := account.NewAccount()
	if err != nil {
		return nil, err
	}
	return a, a.GenOneTimeKeys(50)
}

// receiveRoomKey has another device send alice a Megolm room key over a new
// Olm session (an m.room_key to-device event). It returns the sending side of
// that Megolm session and the session key alice decrypted.
func receiveRoomKey(alice *account.Account) (*session.MegolmOutboundSession, []byte, error) {
	_, aliceKey, err := alice.IdentityKeys()
	if err != nil {
		return nil, nil, err
	}
	otks, err := alice.OneTimeKeys()
	if err != nil {
		return nil, nil, err
	}
	var otk id.Curve25519
	for _, key := range otks {
		otk = key
		break
	}
	bob, err := account.NewAccount()
	if err != nil {
		return nil, nil, err
	}
	toAlice, err := bob.NewOutboundSession(aliceKey, otk)
	if err != nil {
		return nil, nil, err
	}
	group, err := session.NewMegolmOutboundSession()
	if err != nil {
		return nil, nil, err
	}
	content, err := json.Marshal(map[string]any{
		"type": "m.room_key",
		"content": map[string]string{
			"algorithm":   "m.megolm.v1.aes-sha2",
			"room_id":     "!probe:musubee.invalid",
			"session_id":  string(group.ID()),
			"session_key": group.Key(),
		},
	})
	if err != nil {
		return nil, nil, err
	}
	msgType, ciphertext, err := toAlice.Encrypt(content)
	if err != nil {
		return nil, nil, err
	}
	fromBob, err := alice.NewInboundSession(string(ciphertext))
	if err != nil {
		return nil, nil, err
	}
	plain, err := fromBob.Decrypt(string(ciphertext), msgType)
	if err != nil {
		return nil, nil, err
	}
	var event struct {
		Content struct {
			SessionKey string `json:"session_key"`
		} `json:"content"`
	}
	if err = json.Unmarshal(plain, &event); err != nil {
		return nil, nil, err
	}
	return group, []byte(event.Content.SessionKey), nil
}

// decryptMessages has sender encrypt n Megolm messages of about 1 KiB, the
// size of a Matrix text event with its metadata, and decrypts them with the
// session key.
func decryptMessages(sender *session.MegolmOutboundSession, sessionKey []byte, n int) error {
	inbound, err := session.NewMegolmInboundSession(sessionKey)
	if err != nil {
		return err
	}
	body := strings.Repeat("x", 1024)
	for i := range n {
		ciphertext, err := sender.Encrypt([]byte(body))
		if err != nil {
			return err
		}
		plain, index, err := inbound.Decrypt(ciphertext)
		if err != nil {
			return err
		}
		if index != uint(i) || len(plain) != len(body) {
			return fmt.Errorf("message %d decrypted as index %d, %d bytes", i, index, len(plain))
		}
	}
	return nil
}
