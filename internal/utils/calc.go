/*
 * ● YukkiMusic
 * ○ A high-performance engine for streaming music in Telegram voicechats.
 *
 * Copyright (C) 2026 TheTeamVivek
 *
 * This program is free software: you can redistribute it and/or modify it under the
 * terms of the GNU General Public License as published by the Free Software Foundation,
 * either version 3 of the License, or (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful, but WITHOUT ANY
 * WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
 * PARTICULAR PURPOSE. See the GNU General Public License for more details.
 *
 * Repository: https://github.com/TheTeamVivek/YukkiMusic
 */

package utils

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// EvaluateExpression evaluates a basic arithmetic expression string and
// returns the numeric result. It supports +, -, *, /, % (modulo), ^ (power),
// unary minus, decimals, and parentheses. No external dependencies and no
// code execution — this is a plain recursive-descent parser, safe to expose
// to end users via a command like /calc.
func EvaluateExpression(expr string) (float64, error) {
	p := &calcParser{input: []rune(strings.TrimSpace(expr))}
	if len(p.input) == 0 {
		return 0, errors.New("empty expression")
	}

	result, err := p.parseExpr()
	if err != nil {
		return 0, err
	}

	p.skipSpaces()
	if p.pos != len(p.input) {
		return 0, fmt.Errorf("unexpected character %q at position %d", p.input[p.pos], p.pos)
	}

	if math.IsInf(result, 0) || math.IsNaN(result) {
		return 0, errors.New("result is undefined (division by zero or overflow)")
	}

	return result, nil
}

type calcParser struct {
	input []rune
	pos   int
}

func (p *calcParser) skipSpaces() {
	for p.pos < len(p.input) && p.input[p.pos] == ' ' {
		p.pos++
	}
}

func (p *calcParser) peek() rune {
	p.skipSpaces()
	if p.pos >= len(p.input) {
		return 0
	}
	return p.input[p.pos]
}

// expr := term (('+' | '-') term)*
func (p *calcParser) parseExpr() (float64, error) {
	left, err := p.parseTerm()
	if err != nil {
		return 0, err
	}

	for {
		op := p.peek()
		if op != '+' && op != '-' {
			break
		}
		p.pos++
		right, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if op == '+' {
			left += right
		} else {
			left -= right
		}
	}

	return left, nil
}

// term := factor (('*' | '/' | '%') factor)*
func (p *calcParser) parseTerm() (float64, error) {
	left, err := p.parseFactor()
	if err != nil {
		return 0, err
	}

	for {
		op := p.peek()
		if op != '*' && op != '/' && op != '%' {
			break
		}
		p.pos++
		right, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		switch op {
		case '*':
			left *= right
		case '/':
			if right == 0 {
				return 0, errors.New("division by zero")
			}
			left /= right
		case '%':
			if right == 0 {
				return 0, errors.New("modulo by zero")
			}
			left = math.Mod(left, right)
		}
	}

	return left, nil
}

// factor := power ('^' factor)?  (right-associative)
func (p *calcParser) parseFactor() (float64, error) {
	base, err := p.parseUnary()
	if err != nil {
		return 0, err
	}

	if p.peek() == '^' {
		p.pos++
		exp, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		return math.Pow(base, exp), nil
	}

	return base, nil
}

// unary := ('-' | '+')? primary
func (p *calcParser) parseUnary() (float64, error) {
	c := p.peek()
	if c == '-' {
		p.pos++
		val, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		return -val, nil
	}
	if c == '+' {
		p.pos++
		return p.parseUnary()
	}
	return p.parsePrimary()
}

// primary := number | '(' expr ')'
func (p *calcParser) parsePrimary() (float64, error) {
	c := p.peek()

	if c == '(' {
		p.pos++
		val, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		if p.peek() != ')' {
			return 0, errors.New("missing closing parenthesis")
		}
		p.pos++
		return val, nil
	}

	if (c >= '0' && c <= '9') || c == '.' {
		start := p.pos
		for p.pos < len(p.input) &&
			((p.input[p.pos] >= '0' && p.input[p.pos] <= '9') || p.input[p.pos] == '.') {
			p.pos++
		}
		numStr := string(p.input[start:p.pos])
		val, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid number %q", numStr)
		}
		return val, nil
	}

	if c == 0 {
		return 0, errors.New("unexpected end of expression")
	}

	return 0, fmt.Errorf("unexpected character %q", c)
}

// FormatCalcResult trims trailing zeroes for a clean display (e.g. "10" not
// "10.000000", but keeps decimals when they matter).
func FormatCalcResult(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	s := strconv.FormatFloat(v, 'f', 6, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}
