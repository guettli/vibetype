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
