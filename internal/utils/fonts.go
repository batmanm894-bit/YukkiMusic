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

import "strings"

// fontStyle describes how to remap 'A'-'Z', 'a'-'z' and '0'-'9' into a
// stylish unicode block. base* is the starting code point of a contiguous
// range for that character class; use 0 to mean "leave unchanged". overrides
// patches individual characters that fall outside their block's otherwise
// contiguous range (several Unicode "Letterlike Symbols" blocks are
// irregular — e.g. Double-Struck C/H/N/P/Q/R/Z reuse older code points).
type fontStyle struct {
	name        string
	baseUpper   rune
	baseLower   rune
	baseDigit   rune
	overrides   map[rune]rune
}

// FontStyles lists every style supported by /font, in display order.
var fontStyleOrder = []string{
	"bold",
	"italic",
	"bold_italic",
	"script",
	"fraktur",
	"double_struck",
	"sans",
	"sans_bold",
	"monospace",
	"fullwidth",
	"circled",
}

var fontStyles = map[string]fontStyle{
	"bold": {
		name: "Bold", baseUpper: 0x1D400, baseLower: 0x1D41A, baseDigit: 0x1D7CE,
	},
	"italic": {
		name: "Italic", baseUpper: 0x1D434, baseLower: 0x1D44E, baseDigit: 0,
		overrides: map[rune]rune{'h': 0x210E}, // italic h has its own legacy code point
	},
	"bold_italic": {
		name: "Bold Italic", baseUpper: 0x1D468, baseLower: 0x1D482, baseDigit: 0,
	},
	"script": {
		name: "Bold Script", baseUpper: 0x1D4D0, baseLower: 0x1D4EA, baseDigit: 0,
	},
	"fraktur": {
		name: "Bold Fraktur", baseUpper: 0x1D56C, baseLower: 0x1D586, baseDigit: 0,
	},
	"double_struck": {
		name: "Double-Struck", baseUpper: 0x1D538, baseLower: 0x1D552, baseDigit: 0x1D7D8,
		overrides: map[rune]rune{
			'C': 0x2102, 'H': 0x210D, 'N': 0x2115, 'P': 0x2119,
			'Q': 0x211A, 'R': 0x211D, 'Z': 0x2124,
		},
	},
	"sans": {
		name: "Sans-Serif", baseUpper: 0x1D5A0, baseLower: 0x1D5BA, baseDigit: 0x1D7E2,
	},
	"sans_bold": {
		name: "Sans-Serif Bold", baseUpper: 0x1D5D4, baseLower: 0x1D5EE, baseDigit: 0x1D7EC,
	},
	"monospace": {
		name: "Monospace", baseUpper: 0x1D670, baseLower: 0x1D68A, baseDigit: 0x1D7F6,
	},
	"fullwidth": {
		name: "Fullwidth", baseUpper: 0xFF21, baseLower: 0xFF41, baseDigit: 0xFF10,
		overrides: map[rune]rune{' ': 0x3000},
	},
	"circled": {
		name: "Circled", baseUpper: 0x24B6, baseLower: 0x24D0, baseDigit: 0x2460,
		// Ⓐ..Ⓩ / ⓐ..ⓩ are contiguous, but circled digits are irregular:
		// ①..⑨ (U+2460-2468) covers 1-9, while 0 is a separate code point.
		overrides: map[rune]rune{'0': 0x24EA},
	},
}

// FontStyleNames returns the list of valid style keys accepted by ConvertFont,
// in a friendly display order (used by /fonts).
func FontStyleNames() []string {
	names := make([]string, len(fontStyleOrder))
	copy(names, fontStyleOrder)
	return names
}

// FontStyleLabel returns the human-readable name for a style key.
func FontStyleLabel(style string) string {
	if s, ok := fontStyles[style]; ok {
		return s.name
	}
	return style
}

// ConvertFont converts text into the given stylish unicode font. Characters
// with no mapping in that style (punctuation, digits for italic, etc.) are
// left unchanged so the output stays readable.
func ConvertFont(text, style string) (string, error) {
	fs, ok := fontStyles[style]
	if !ok {
		return "", errUnknownFontStyle(style)
	}

	var sb strings.Builder
	for _, r := range text {
		if or, ok := fs.overrides[r]; ok {
			sb.WriteRune(or)
			continue
		}
		switch {
		case r >= 'A' && r <= 'Z' && fs.baseUpper != 0:
			sb.WriteRune(fs.baseUpper + (r - 'A'))
		case r >= 'a' && r <= 'z' && fs.baseLower != 0:
			sb.WriteRune(fs.baseLower + (r - 'a'))
		case r >= '0' && r <= '9' && fs.baseDigit != 0:
			sb.WriteRune(fs.baseDigit + (r - '0'))
		default:
			sb.WriteRune(r)
		}
	}

	return sb.String(), nil
}

// ConvertFontAll renders the given text in every supported style, keyed by
// style name — handy for a /font preview showing all options at once.
func ConvertFontAll(text string) map[string]string {
	out := make(map[string]string, len(fontStyleOrder))
	for _, key := range fontStyleOrder {
		converted, _ := ConvertFont(text, key)
		out[fontStyles[key].name] = converted
	}
	return out
}

type unknownFontStyleErr struct{ style string }

func (e unknownFontStyleErr) Error() string {
	return "unknown font style: " + e.style
}

func errUnknownFontStyle(style string) error {
	return unknownFontStyleErr{style: style}
}

