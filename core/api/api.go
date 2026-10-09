// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package api is the contract between the core and its user interfaces
// (docs/ADR/0012-core-api-contract.md). The source of truth is the JSON
// Schema in schema/core-api.schema.json; the Go types of this package
// (types.gen.go) and the TypeScript types of the UI
// (ui/src/core-api/types.gen.ts) are generated from it:
//
//	go generate ./core/api
//
// The package holds no logic: the core implements the commands (package
// embedded), and the transports carry the JSON (package ffi, JNI).
package api

//go:generate go run ./apigen

import (
	_ "embed"
	"reflect"
)

// Schema is the JSON Schema of the contract.
//
//go:embed schema/core-api.schema.json
var Schema []byte

// CommandSpec is one command and the Go types of its params and result.
type CommandSpec struct {
	Name   string
	Params reflect.Type
	Result reflect.Type
}

// EventSpec is one event type and the Go type of its data.
type EventSpec struct {
	Type string
	Data reflect.Type
}

// Request is the envelope of a request sent to the core.
type Request struct {
	ID      int64          `json:"id"`
	Command string         `json:"command"`
	Params  map[string]any `json:"params,omitempty"`
}

// Event is the envelope of an event sent by the core.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Error makes a CoreError usable as a Go error.
func (e *CoreError) Error() string {
	return string(e.Code) + ": " + e.Message
}
