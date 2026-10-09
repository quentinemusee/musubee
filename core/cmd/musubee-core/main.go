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
// It exits when its standard input ends: when the app closes it, and also
// when the app dies, since the system then closes the app's end of the pipe.
// An interrupt or termination signal closes the core cleanly too.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
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
	config, err := json.Marshal(embedded.Config{DataDir: dataDir, LogLevel: logLevel})
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
