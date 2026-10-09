// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package codegen turns the core API schema (core/api/schema) into Go and
// TypeScript types, so that both sides of the contract come from the same
// file (docs/ADR/0012-core-api-contract.md).
//
// It understands a small subset of JSON Schema on purpose: named object and
// string enum types in $defs, properties of primitive, array, map or $ref
// type, and the $defs/Commands and $defs/Events tables. Validation-only
// keywords (pattern, minimum, if/then...) are ignored, since they do not
// change the types; any other keyword is an error, so that the schema can
// never say more than the generated types without anyone noticing.
package codegen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Schema is the part of the API schema the generators need.
type Schema struct {
	Version  string
	Types    []*Type
	Commands []*Command
	Events   []*Event
}

// Command is one entry of $defs/Commands.
type Command struct {
	Name        string
	Description string
	Params      string
	Result      string
}

// Event is one entry of $defs/Events.
type Event struct {
	Name        string
	Description string
	Data        string
}

// Type is a named type of $defs: an object (Fields) or a string enum (Enum).
type Type struct {
	Name        string
	Description string
	Fields      []*Field
	Enum        []string
}

// IsEnum reports whether t is a string enum.
func (t *Type) IsEnum() bool { return t.Enum != nil }

// Field is a property of an object type.
type Field struct {
	JSONName    string
	Description string
	Type        *TypeRef
	Required    bool
}

// TypeRef is the type of a field: exactly one of the fields is set.
type TypeRef struct {
	// Named is the name of a type of $defs.
	Named string
	// Primitive is "string", "integer", "number" or "boolean".
	Primitive string
	// Array is the type of the items of an array.
	Array *TypeRef
	// Map is the type of the values of an object with arbitrary keys.
	Map *TypeRef
}

// envelopeDefs are validated by the contract tests but not generated: the
// generators write the envelope types themselves, typed by command.
var envelopeDefs = map[string]bool{"Request": true, "Response": true, "Event": true}

// Keywords that only constrain values, and do not change the types.
var validationOnly = map[string]bool{
	"description": true, "pattern": true, "minLength": true, "maxLength": true,
	"minimum": true, "maximum": true, "minItems": true, "maxItems": true,
	"format": true, "allOf": true, "$comment": true, "examples": true,
}

// Parse reads the API schema.
func Parse(data []byte) (*Schema, error) {
	root, err := parseOrdered(data)
	if err != nil {
		return nil, err
	}
	top, ok := root.(*object)
	if !ok {
		return nil, fmt.Errorf("the schema is not an object")
	}
	s := &Schema{}
	if s.Version, ok = top.get("x-musubee-api-version").(string); !ok {
		return nil, fmt.Errorf("x-musubee-api-version is missing")
	}
	defs, ok := top.get("$defs").(*object)
	if !ok {
		return nil, fmt.Errorf("$defs is missing")
	}
	for _, name := range defs.keys {
		def, ok := defs.vals[name].(*object)
		if !ok {
			return nil, fmt.Errorf("$defs/%s is not an object", name)
		}
		switch {
		case envelopeDefs[name]:
		case name == "Commands":
			if err = s.parseCommands(def); err != nil {
				return nil, err
			}
		case name == "Events":
			if err = s.parseEvents(def); err != nil {
				return nil, err
			}
		default:
			t, err := parseType(name, def)
			if err != nil {
				return nil, fmt.Errorf("$defs/%s: %w", name, err)
			}
			s.Types = append(s.Types, t)
		}
	}
	return s, s.check()
}

func (s *Schema) parseCommands(def *object) error {
	props, ok := def.get("properties").(*object)
	if !ok {
		return fmt.Errorf("$defs/Commands has no properties")
	}
	for _, name := range props.keys {
		cmd, ok := props.vals[name].(*object)
		if !ok {
			return fmt.Errorf("command %s is not an object", name)
		}
		parts, ok := cmd.get("properties").(*object)
		if !ok {
			return fmt.Errorf("command %s has no properties", name)
		}
		params, err := refName(parts.get("params"))
		if err != nil {
			return fmt.Errorf("command %s params: %w", name, err)
		}
		result, err := refName(parts.get("result"))
		if err != nil {
			return fmt.Errorf("command %s result: %w", name, err)
		}
		desc, _ := cmd.get("description").(string)
		s.Commands = append(s.Commands, &Command{Name: name, Description: desc, Params: params, Result: result})
	}
	return nil
}

func (s *Schema) parseEvents(def *object) error {
	props, ok := def.get("properties").(*object)
	if !ok {
		return fmt.Errorf("$defs/Events has no properties")
	}
	for _, name := range props.keys {
		data, err := refName(props.vals[name])
		if err != nil {
			return fmt.Errorf("event %s: %w", name, err)
		}
		desc, _ := props.vals[name].(*object).get("description").(string)
		s.Events = append(s.Events, &Event{Name: name, Description: desc, Data: data})
	}
	return nil
}

func refName(v any) (string, error) {
	obj, ok := v.(*object)
	if !ok {
		return "", fmt.Errorf("expected an object with $ref")
	}
	ref, ok := obj.get("$ref").(string)
	if !ok || !strings.HasPrefix(ref, "#/$defs/") {
		return "", fmt.Errorf("expected a $ref to #/$defs/")
	}
	return strings.TrimPrefix(ref, "#/$defs/"), nil
}

func parseType(name string, def *object) (*Type, error) {
	t := &Type{Name: name}
	t.Description, _ = def.get("description").(string)
	switch def.get("type") {
	case "string":
		enum, ok := def.get("enum").([]any)
		if !ok || len(enum) == 0 {
			return nil, fmt.Errorf("a named string type must be an enum")
		}
		for _, v := range enum {
			str, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("enum values must be strings")
			}
			t.Enum = append(t.Enum, str)
		}
		return t, checkKeywords(def, "type", "enum")
	case "object":
		if def.get("additionalProperties") != false {
			return nil, fmt.Errorf("named objects must set additionalProperties to false")
		}
		required := map[string]bool{}
		if list, ok := def.get("required").([]any); ok {
			for _, v := range list {
				required[v.(string)] = true
			}
		}
		props, _ := def.get("properties").(*object)
		if props != nil {
			for _, key := range props.keys {
				prop, ok := props.vals[key].(*object)
				if !ok {
					return nil, fmt.Errorf("property %s is not an object", key)
				}
				ref, err := parseRef(prop)
				if err != nil {
					return nil, fmt.Errorf("property %s: %w", key, err)
				}
				desc, _ := prop.get("description").(string)
				t.Fields = append(t.Fields, &Field{JSONName: key, Description: desc, Type: ref, Required: required[key]})
				delete(required, key)
			}
		}
		for key := range required {
			return nil, fmt.Errorf("required property %s is not defined", key)
		}
		return t, checkKeywords(def, "type", "properties", "required", "additionalProperties")
	default:
		return nil, fmt.Errorf("named types must be objects or string enums")
	}
}

func parseRef(prop *object) (*TypeRef, error) {
	if prop.get("$ref") != nil {
		name, err := refName(prop)
		if err != nil {
			return nil, err
		}
		return &TypeRef{Named: name}, checkKeywords(prop, "$ref")
	}
	switch typ := prop.get("type"); typ {
	case "string", "integer", "number", "boolean":
		if prop.get("enum") != nil {
			return nil, fmt.Errorf("enums must be named types in $defs")
		}
		return &TypeRef{Primitive: typ.(string)}, checkKeywords(prop, "type")
	case "array":
		items, ok := prop.get("items").(*object)
		if !ok {
			return nil, fmt.Errorf("an array needs items")
		}
		elem, err := parseRef(items)
		if err != nil {
			return nil, err
		}
		return &TypeRef{Array: elem}, checkKeywords(prop, "type", "items")
	case "object":
		values, ok := prop.get("additionalProperties").(*object)
		if !ok || prop.get("properties") != nil {
			return nil, fmt.Errorf("inline objects must be maps (additionalProperties with a schema); name the others in $defs")
		}
		elem, err := parseRef(values)
		if err != nil {
			return nil, err
		}
		return &TypeRef{Map: elem}, checkKeywords(prop, "type", "additionalProperties")
	default:
		return nil, fmt.Errorf("unsupported type %v", typ)
	}
}

// checkKeywords rejects the keywords the generators do not understand.
func checkKeywords(obj *object, allowed ...string) error {
	for _, key := range obj.keys {
		if validationOnly[key] || contains(allowed, key) {
			continue
		}
		return fmt.Errorf("unsupported keyword %q", key)
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// check verifies that every reference points to a defined type, and that
// commands and events refer to objects.
func (s *Schema) check() error {
	types := map[string]*Type{}
	for _, t := range s.Types {
		types[t.Name] = t
	}
	var checkRef func(where string, r *TypeRef) error
	checkRef = func(where string, r *TypeRef) error {
		switch {
		case r.Array != nil:
			return checkRef(where, r.Array)
		case r.Map != nil:
			return checkRef(where, r.Map)
		case r.Named != "" && types[r.Named] == nil:
			return fmt.Errorf("%s refers to the undefined type %s", where, r.Named)
		}
		return nil
	}
	for _, t := range s.Types {
		for _, f := range t.Fields {
			if err := checkRef(t.Name+"."+f.JSONName, f.Type); err != nil {
				return err
			}
		}
	}
	isObject := func(name string) bool { return types[name] != nil && !types[name].IsEnum() }
	for _, c := range s.Commands {
		if !isObject(c.Params) || !isObject(c.Result) {
			return fmt.Errorf("command %s must refer to object types", c.Name)
		}
	}
	for _, e := range s.Events {
		if !isObject(e.Data) {
			return fmt.Errorf("event %s must refer to an object type", e.Name)
		}
	}
	if len(s.Commands) == 0 || len(s.Events) == 0 {
		return fmt.Errorf("the schema has no commands or no events")
	}
	return nil
}

// object is a JSON object that remembers the order of its keys, so that the
// generated code follows the order of the schema.
type object struct {
	keys []string
	vals map[string]any
}

func (o *object) get(key string) any { return o.vals[key] }

func parseOrdered(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err = dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("unexpected data after the schema")
	}
	return v, nil
}

func parseValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch tok {
	case json.Delim('{'):
		obj := &object{vals: map[string]any{}}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return nil, err
			}
			k := key.(string)
			if _, dup := obj.vals[k]; dup {
				return nil, fmt.Errorf("duplicate key %q", k)
			}
			if obj.vals[k], err = parseValue(dec); err != nil {
				return nil, err
			}
			obj.keys = append(obj.keys, k)
		}
		_, err = dec.Token()
		return obj, err
	case json.Delim('['):
		list := []any{}
		for dec.More() {
			v, err := parseValue(dec)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		_, err = dec.Token()
		return list, err
	default:
		return tok, nil
	}
}

// Generate parses the schema and returns the Go code of package api and the
// TypeScript module.
func Generate(schema []byte) (goCode, tsCode []byte, err error) {
	s, err := Parse(schema)
	if err != nil {
		return nil, nil, err
	}
	if goCode, err = GoPackage(s, "api"); err != nil {
		return nil, nil, err
	}
	return goCode, TypeScript(s), nil
}
