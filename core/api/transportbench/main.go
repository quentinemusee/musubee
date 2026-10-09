// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command transportbench serves a core over a stream, for the transport
// measurements of docs/ADR/0012 (scripts/transportbench). It is a
// measurement tool: the desktop app runs core/cmd/musubee-core.
//
// The stream carries newline-delimited JSON (package stream).
//
//	transportbench -data DIR -listen stdio
//	transportbench -data DIR -listen tcp
//
// With tcp, the server listens on a random loopback port, prints
// "PORT TOKEN" on stdout, and serves one connection whose first line is
// the token: any local process can reach a loopback port, so a local
// socket needs such a secret.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/quentinemusee/musubee/core/api/stream"
	"github.com/quentinemusee/musubee/core/embedded"
)

func main() {
	dataDir := flag.String("data", "", "data directory of the core")
	listen := flag.String("listen", "stdio", "stdio or tcp")
	flag.Parse()
	if err := run(*dataDir, *listen); err != nil {
		log.Fatal(err)
	}
}

func run(dataDir, listen string) error {
	config, err := json.Marshal(embedded.Config{DataDir: dataDir, LogLevel: "warn"})
	if err != nil {
		return err
	}
	core, err := embedded.Open(config)
	if err != nil {
		return err
	}
	defer func() { _ = core.Close() }()
	switch listen {
	case "stdio":
		return stream.Serve(core, embedded.ClosedEvent(), os.Stdin, os.Stdout)
	case "tcp":
		return serveTCP(core)
	}
	return fmt.Errorf("unknown -listen %q", listen)
}

func serveTCP(core *embedded.Core) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()
	secret := make([]byte, 16)
	_, _ = rand.Read(secret)
	token := hex.EncodeToString(secret)
	fmt.Printf("%d %s\n", ln.Addr().(*net.TCPAddr).Port, token)
	conn, err := ln.Accept()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := reader.ReadString('\n')
	if err != nil || line != token+"\n" {
		return fmt.Errorf("wrong token from %s", conn.RemoteAddr())
	}
	_ = conn.SetReadDeadline(time.Time{})
	return stream.Serve(core, embedded.ClosedEvent(), reader, conn)
}
