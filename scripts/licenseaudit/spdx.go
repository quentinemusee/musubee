// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"strings"
	"unicode"
)

// NormalizeID returns the canonical form of a license identifier: legacy
// "+" suffixes become "-or-later".
func NormalizeID(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasSuffix(id, "+") {
		id = strings.TrimSuffix(id, "+") + "-or-later"
	}
	return id
}

// EvalExpression reports whether an SPDX license expression is acceptable,
// given a predicate for single license identifiers. "A OR B" needs one
// acceptable side, "A AND B" needs both, and "A WITH exception" is judged on
// A (exceptions only add permissions). Keywords are case-insensitive, since
// npm metadata often writes "MIT or GPL-2.0".
func EvalExpression(expr string, allowed func(id string) bool) (bool, error) {
	tokens, err := tokenize(expr)
	if err != nil {
		return false, err
	}
	if len(tokens) == 0 {
		return false, fmt.Errorf("empty license expression")
	}
	p := &parser{tokens: tokens, allowed: allowed}
	ok, err := p.or()
	if err != nil {
		return false, err
	}
	if p.pos != len(p.tokens) {
		return false, fmt.Errorf("unexpected %q in license expression %q", p.tokens[p.pos], expr)
	}
	return ok, nil
}

// IDs returns the license identifiers mentioned in an expression.
func IDs(expr string) []string {
	tokens, _ := tokenize(expr)
	var ids []string
	for _, t := range tokens {
		switch strings.ToUpper(t) {
		case "(", ")", "AND", "OR", "WITH":
		default:
			ids = append(ids, NormalizeID(t))
		}
	}
	return ids
}

func tokenize(expr string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	for _, r := range expr {
		switch {
		case r == '(' || r == ')':
			flush()
			tokens = append(tokens, string(r))
		case unicode.IsSpace(r):
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return tokens, nil
}

type parser struct {
	tokens  []string
	pos     int
	allowed func(string) bool
}

func (p *parser) peekKeyword(word string) bool {
	return p.pos < len(p.tokens) && strings.EqualFold(p.tokens[p.pos], word)
}

func (p *parser) or() (bool, error) {
	left, err := p.and()
	if err != nil {
		return false, err
	}
	for p.peekKeyword("OR") {
		p.pos++
		right, err := p.and()
		if err != nil {
			return false, err
		}
		left = left || right
	}
	return left, nil
}

func (p *parser) and() (bool, error) {
	left, err := p.with()
	if err != nil {
		return false, err
	}
	for p.peekKeyword("AND") {
		p.pos++
		right, err := p.with()
		if err != nil {
			return false, err
		}
		left = left && right
	}
	return left, nil
}

func (p *parser) with() (bool, error) {
	value, err := p.atom()
	if err != nil {
		return false, err
	}
	if p.peekKeyword("WITH") {
		p.pos += 2 // the exception identifier itself is not judged
		if p.pos > len(p.tokens) {
			return false, fmt.Errorf("WITH without an exception identifier")
		}
	}
	return value, nil
}

func (p *parser) atom() (bool, error) {
	if p.pos >= len(p.tokens) {
		return false, fmt.Errorf("license expression ends too early")
	}
	token := p.tokens[p.pos]
	p.pos++
	switch {
	case token == "(":
		value, err := p.or()
		if err != nil {
			return false, err
		}
		if p.pos >= len(p.tokens) || p.tokens[p.pos] != ")" {
			return false, fmt.Errorf("missing closing parenthesis")
		}
		p.pos++
		return value, nil
	case token == ")" || strings.EqualFold(token, "AND") || strings.EqualFold(token, "OR") || strings.EqualFold(token, "WITH"):
		return false, fmt.Errorf("unexpected %q", token)
	default:
		return p.allowed(NormalizeID(token)), nil
	}
}
