// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package stream serves a core over a byte stream of newline-delimited JSON
// (docs/ADR/0012-core-api-contract.md): the client writes one request per
// line, the core writes responses (which have an "id") and events (which
// have a "type") as they come, one per line. It is the transport of the
// desktop app, where the core runs in a child process and the stream is its
// standard input and output (docs/ADR/0013-desktop-shell.md).
package stream

import (
	"bufio"
	"bytes"
	"io"
	"sync"
	"time"
)

// MaxLineSize is the size of the longest request Serve reads. A longer line
// ends the stream: the client is broken.
const MaxLineSize = 16 << 20

// Core is what Serve needs of a core; *embedded.Core implements it.
type Core interface {
	Call(request []byte) []byte
	NextEvent(timeout time.Duration) ([]byte, bool)
	Close() error
}

// Serve runs the requests read from r, concurrently as with the C library,
// and writes their responses and the core's events to w. When r ends, it
// closes the core, which ends the requests still running (login.wait has no
// time limit of its own), writes their responses and the final core.closed
// event, and returns. Errors writing to w are ignored: a client that stops
// reading also closes r.
func Serve(core Core, closedEvent []byte, r io.Reader, w io.Writer) error {
	out := bufio.NewWriter(w)
	var mu sync.Mutex
	write := func(data []byte) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = out.Write(data)
		_ = out.WriteByte('\n')
		_ = out.Flush()
	}

	var calls sync.WaitGroup
	events := make(chan struct{})
	go func() {
		defer close(events)
		for {
			data, ok := core.NextEvent(time.Minute)
			if !ok {
				continue
			}
			if bytes.Equal(data, closedEvent) {
				// Every response is written before the last event.
				calls.Wait()
				write(data)
				return
			}
			write(data)
		}
	}()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), MaxLineSize)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		request := bytes.Clone(scanner.Bytes())
		calls.Go(func() { write(core.Call(request)) })
	}
	readErr := scanner.Err()
	closeErr := core.Close()
	<-events
	if readErr != nil {
		return readErr
	}
	return closeErr
}
