// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command musubee-core runs the core in its own process, for the desktop app
// (docs/ADR/0013-desktop-shell.md). It speaks the core API (docs/ADR/0012)
// as newline-delimited JSON on its standard input and output (package
// stream), and logs to core.log in its data directory, never to the
// standard streams.
//
//	musubee-core -data DIR [-log-level info] [-key-from-stdin]
//
// With -key-from-stdin, the first line of the standard input is the master
// key of the secrets at rest, 32 bytes in base64, before the API stream:
// the desktop app keeps it in the system's secure storage on macOS and
// Linux (docs/ADR/0027). The standard input rather than a flag or an
// environment variable, so that the key shows neither in the system's list
// of processes nor in the environment of the process. Without it, the core
// keeps its own key file in the data directory (docs/ADR/0019).
//
// The Telegram network is offered when MUSUBEE_TG_API_ID and
// MUSUBEE_TG_API_HASH hold the application's credentials
// (https://my.telegram.org): environment variables rather than flags, so
// that they do not show in the system's list of processes.
//
// It exits when its standard input ends: when the app closes it, and also
// when the app dies, since the system then closes the app's end of the pipe.
// An interrupt or termination signal closes the core cleanly too.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/quentinemusee/musubee/core/api/stream"
	"github.com/quentinemusee/musubee/core/embedded"
	"github.com/quentinemusee/musubee/core/secrets"
)

func main() {
	dataDir := flag.String("data", "", "data directory of the core (required)")
	logLevel := flag.String("log-level", "info", "level of core.log: debug, info, warn or error")
	keyFromStdin := flag.Bool("key-from-stdin", false, "read the master key (base64) from the first line of the standard input")
	flag.Parse()
	if *dataDir == "" || flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*dataDir, *logLevel, *keyFromStdin); err != nil {
		fmt.Fprintln(os.Stderr, "musubee-core:", err)
		os.Exit(1)
	}
}

func run(dataDir, logLevel string, keyFromStdin bool) error {
	telegram, err := telegramConfig(os.Getenv)
	if err != nil {
		return err
	}
	input := bufio.NewReader(os.Stdin)
	var databaseKey string
	if keyFromStdin {
		if databaseKey, err = readKey(input); err != nil {
			return err
		}
	}
	config, err := json.Marshal(embedded.Config{DataDir: dataDir, LogLevel: logLevel, Telegram: telegram, DatabaseKey: databaseKey})
	if err != nil {
		return err
	}
	core, err := embedded.Open(config)
	if err != nil {
		return err
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		// Serve writes core.closed once the core is closed, but keeps
		// waiting for the input: exit here.
		code := 0
		if err := core.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "musubee-core:", err)
			code = 1
		}
		os.Exit(code)
	}()
	return stream.Serve(core, embedded.ClosedEvent(), input, os.Stdout)
}

// readKey reads the master key from the first line of the input. The error
// never quotes the line.
func readKey(input *bufio.Reader) (string, error) {
	line, err := input.ReadString('\n')
	if err != nil {
		return "", errors.New("-key-from-stdin: no key line on the standard input")
	}
	key := strings.TrimRight(line, "\r\n")
	if _, err := secrets.ParseKey(key); err != nil {
		return "", fmt.Errorf("-key-from-stdin: %w", err)
	}
	return key, nil
}

// telegramConfig reads the Telegram credentials from the environment: none
// when both variables are unset, an error when only one is set or the ID is
// not a number. The error never quotes the values.
func telegramConfig(getenv func(string) string) (*embedded.TelegramConfig, error) {
	id, hash := getenv("MUSUBEE_TG_API_ID"), getenv("MUSUBEE_TG_API_HASH")
	if id == "" && hash == "" {
		return nil, nil
	}
	apiID, err := strconv.Atoi(id)
	if err != nil || apiID <= 0 || hash == "" {
		return nil, errors.New("MUSUBEE_TG_API_ID must be a positive number and MUSUBEE_TG_API_HASH must be set")
	}
	return &embedded.TelegramConfig{APIID: apiID, APIHash: hash}, nil
}
