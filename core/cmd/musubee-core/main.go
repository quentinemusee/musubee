// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command musubee-core runs the core in its own process, for the desktop app
// (docs/ADR/0013-desktop-shell.md). It speaks the core API (docs/ADR/0012)
// as newline-delimited JSON on its standard input and output (package
// stream), and logs to core.log in its data directory, never to the
// standard streams.
//
//	musubee-core -data DIR [-log-level info]
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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/quentinemusee/musubee/core/api/stream"
	"github.com/quentinemusee/musubee/core/embedded"
)

func main() {
	dataDir := flag.String("data", "", "data directory of the core (required)")
	logLevel := flag.String("log-level", "info", "level of core.log: debug, info, warn or error")
	flag.Parse()
	if *dataDir == "" || flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*dataDir, *logLevel); err != nil {
		fmt.Fprintln(os.Stderr, "musubee-core:", err)
		os.Exit(1)
	}
}

func run(dataDir, logLevel string) error {
	telegram, err := telegramConfig(os.Getenv)
	if err != nil {
		return err
	}
	config, err := json.Marshal(embedded.Config{DataDir: dataDir, LogLevel: logLevel, Telegram: telegram})
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
	return stream.Serve(core, embedded.ClosedEvent(), os.Stdin, os.Stdout)
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
