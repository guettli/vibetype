// Package textnorm normalizes user supplied texts so that every character
// can be typed on a regular (German or US) keyboard.
package textnorm

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

var replacer = strings.NewReplacer(
	"\r\n", "\n",
	"\r", "\n",
	"\t", " ",
	" ", " ", // no-break space
	" ", " ", // narrow no-break space
	" ", " ", // thin space
	"​", "", // zero width space
	"­", "", // soft hyphen
	"\ufeff", "", // BOM
	"“", `"`, "”", `"`, "„", `"`, "«", `"`, "»", `"`,
	"‘", "'", "’", "'", "‚", "'", "‹", "'", "›", "'",
	"–", "-", "—", "-", "−", "-",
	"…", "...",
)

// Normalize returns the canonical form of a text:
//   - Unicode NFC (so "ä" is one code point, like the browser's KeyboardEvent.key)
//   - typographic quotes, dashes, ellipsis and special spaces mapped to ASCII
//   - runs of spaces collapsed, lines trimmed
//   - empty lines removed (one newline == one Enter key press)
//   - no leading/trailing whitespace
func Normalize(s string) string {
	s = norm.NFC.String(s)
	s = replacer.Replace(s)
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.Join(strings.Fields(l), " ")
		if l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// Typable reports whether r can be typed on a regular German or US keyboard
// without dead-key gymnastics: printable ASCII, newline, Latin-1 letters and
// symbols (ä ö ü ß é ç § ° …) and the euro sign.
func Typable(r rune) bool {
	switch {
	case r == '\n', r == '€':
		return true
	case r >= 0x20 && r <= 0x7e:
		return true
	case r >= 0xa1 && r <= 0xff && r != 0xad:
		return true
	}
	return false
}

// Untypable returns the distinct characters of s that are not Typable, in order
// of first appearance.
func Untypable(s string) []rune {
	var out []rune
	seen := map[rune]bool{}
	for _, r := range s {
		if !Typable(r) && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}
