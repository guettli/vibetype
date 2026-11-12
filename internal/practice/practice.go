// Package practice builds personal exercise texts for a user's weak keys.
package practice

import (
	"math/rand/v2"
	"slices"
	"strings"
	"unicode"

	"github.com/guettli/vibetype/internal/lessons"
)

// wordPool is every distinct word of the built-in lessons (punctuation trimmed).
var wordPool = func() []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lessons.All {
		lines := strings.Split(l.Content, "\n")
		for _, w := range strings.Fields(strings.Join(lines[min(l.DrillLines, len(lines)):], " ")) {
			w = strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
			if len([]rune(w)) < 2 || seen[w] {
				continue
			}
			seen[w] = true
			out = append(out, w)
		}
	}
	slices.Sort(out)
	return out
}()

const wordsPerLine = 8

// Text returns an exercise with one paragraph per weak key: words from the
// lessons that contain the key, or a drill pattern if no word does (digits,
// symbols, space). rnd makes the choice reproducible in tests.
func Text(keys []string, rnd *rand.Rand) string {
	var lines []string
	for _, k := range keys {
		if k == " " || k == "\n" {
			continue // practised by every line anyway
		}
		var matches []string
		for _, w := range wordPool {
			if strings.Contains(w, k) {
				matches = append(matches, w)
			}
		}
		if len(matches) == 0 {
			lines = append(lines, drill(k), drill(k))
			continue
		}
		for line := 0; line < 2; line++ {
			// few matching words (e.g. ö): repeat them so every line has wordsPerLine words
			words := make([]string, 0, wordsPerLine)
			for len(words) < wordsPerLine {
				rnd.Shuffle(len(matches), func(i, j int) { matches[i], matches[j] = matches[j], matches[i] })
				words = append(words, matches[:min(wordsPerLine-len(words), len(matches))]...)
			}
			lines = append(lines, strings.Join(words, " "))
		}
	}
	return strings.Join(lines, "\n")
}

// drill repeats the key in short groups, e.g. "%% a% %a %%".
func drill(k string) string {
	return strings.Join([]string{k + k, "a" + k, k + "a", k + k + k, "f" + k, k + "j", k + k}, " ")
}
