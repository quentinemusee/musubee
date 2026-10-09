// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package codegen

import (
	"bytes"
	"fmt"
	"strings"
)

// TypeScript generates the TypeScript types of the schema, as one module.
func TypeScript(s *Schema) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "// %s\n// %s\n\n// %s\n\n", spdxCopyright, spdxLicense, generatedBy)
	tsComment(&b, "", "Version of the contract, \"major.minor\".")
	fmt.Fprintf(&b, "export const API_VERSION = %q;\n", s.Version)

	for _, t := range s.Types {
		b.WriteString("\n")
		tsComment(&b, "", t.Description)
		switch {
		case t.IsEnum():
			quoted := make([]string, len(t.Enum))
			for i, v := range t.Enum {
				quoted[i] = fmt.Sprintf("%q", v)
			}
			line := fmt.Sprintf("export type %s = %s;\n", t.Name, strings.Join(quoted, " | "))
			if len(line) > 80 {
				line = fmt.Sprintf("export type %s =\n  | %s;\n", t.Name, strings.Join(quoted, "\n  | "))
			}
			b.WriteString(line)
		case len(t.Fields) == 0:
			fmt.Fprintf(&b, "export type %s = Record<string, never>;\n", t.Name)
		default:
			fmt.Fprintf(&b, "export interface %s {\n", t.Name)
			for _, f := range t.Fields {
				tsComment(&b, "  ", f.Description)
				optional := ""
				if !f.Required {
					optional = "?"
				}
				fmt.Fprintf(&b, "  %s%s: %s;\n", f.JSONName, optional, tsType(f.Type))
			}
			b.WriteString("}\n")
		}
	}

	b.WriteString("\n")
	tsComment(&b, "", "Every command, by name, with the types of its params and result.")
	b.WriteString("export interface Commands {\n")
	for _, c := range s.Commands {
		tsComment(&b, "  ", c.Description)
		fmt.Fprintf(&b, "  %q: { params: %s; result: %s };\n", c.Name, c.Params, c.Result)
	}
	b.WriteString("}\n\n")
	tsComment(&b, "", "Every event, by type, with the type of its data.")
	b.WriteString("export interface Events {\n")
	for _, e := range s.Events {
		tsComment(&b, "  ", e.Description)
		fmt.Fprintf(&b, "  %q: %s;\n", e.Name, e.Data)
	}
	b.WriteString("}\n\n")

	b.WriteString("export type CommandName = keyof Commands;\nexport type EventType = keyof Events;\n\n")
	b.WriteString("export const COMMAND_NAMES: readonly CommandName[] = [\n")
	for _, c := range s.Commands {
		fmt.Fprintf(&b, "  %q,\n", c.Name)
	}
	b.WriteString("];\n\nexport const EVENT_TYPES: readonly EventType[] = [\n")
	for _, e := range s.Events {
		fmt.Fprintf(&b, "  %q,\n", e.Name)
	}
	b.WriteString("];\n\n")

	b.WriteString(`/** Envelope of a request sent to the core. */
export interface Request<K extends CommandName = CommandName> {
  id: number;
  command: K;
  params?: Commands[K]["params"];
}

/** Envelope of the core's answer: exactly one of result and error. */
export type Response<K extends CommandName = CommandName> =
  | { id: number; result: Commands[K]["result"]; error?: never }
  | { id: number; error: CoreError; result?: never };

/** Envelope of an event sent by the core, discriminated by type. */
export type Event = { [K in EventType]: { type: K; data: Events[K] } }[EventType];
`)
	return b.Bytes()
}

func tsType(r *TypeRef) string {
	switch {
	case r.Named != "":
		return r.Named
	case r.Array != nil:
		return tsType(r.Array) + "[]"
	case r.Map != nil:
		return "Record<string, " + tsType(r.Map) + ">"
	}
	return map[string]string{"string": "string", "integer": "number", "number": "number", "boolean": "boolean"}[r.Primitive]
}

// tsComment writes text as a JSDoc comment, wrapped at about 80 columns.
func tsComment(b *bytes.Buffer, indent, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	lines := wrap(strings.ReplaceAll(text, "*/", "* /"), 76-len(indent))
	if len(lines) == 1 && len(lines[0])+len(indent)+7 <= 80 {
		fmt.Fprintf(b, "%s/** %s */\n", indent, lines[0])
		return
	}
	fmt.Fprintf(b, "%s/**\n", indent)
	for _, line := range lines {
		fmt.Fprintf(b, "%s * %s\n", indent, line)
	}
	fmt.Fprintf(b, "%s */\n", indent)
}
