// Package practice builds personal exercise texts for a user's weak keys.
package practice

import (
	"math/rand/v2"
	"slices"
	"strings"
	"unicode"

	"github.com/guettli/vibetype/internal/lessons"
)

// wordPool is every distinct word of the built-in lessons (punctuation
// trimmed), without the finger-drill lines.
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

// Text returns an exercise for the given weak keys: two lines per key with
// lesson words that contain it (padded with drills if there are few such
// words), short words for the space bar, many short lines for Enter. rnd makes
// the choice reproducible in tests.
func Text(keys []string, rnd *rand.Rand) string {
	var lines []string
	for _, k := range keys {
		switch k {
		case " ":
			short := filter(func(w string) bool { return len([]rune(w)) <= 3 && w == strings.ToLower(w) })
			lines = append(lines, pick(short, wordsPerLine+4, rnd), pick(short, wordsPerLine+4, rnd))
			continue
		case "\n":
			short := filter(func(w string) bool { return len([]rune(w)) <= 4 })
			for range 6 {
				lines = append(lines, pick(short, 2, rnd))
			}
			continue
		}
		matches := filter(func(w string) bool { return strings.Contains(w, k) })
		if len(matches) >= 3 {
			lines = append(lines, pick(matches, wordsPerLine, rnd), pick(matches, wordsPerLine, rnd))
			continue
		}
		// few or no words (digits, symbols, rare letters): words plus drill groups
		if len(matches) > 0 {
			lines = append(lines, pick(matches, len(matches), rnd)+" "+drill(k, rnd))
		} else {
			lines = append(lines, drill(k, rnd))
		}
		lines = append(lines, drill(k, rnd))
	}
	return strings.Join(lines, "\n")
}

func filter(keep func(string) bool) []string {
	var out []string
	for _, w := range wordPool {
		if keep(w) {
			out = append(out, w)
		}
	}
	return out
}

// pick returns n words from words (repeating them if there are fewer), shuffled.
func pick(words []string, n int, rnd *rand.Rand) string {
	ws := slices.Clone(words)
	out := make([]string, 0, n)
	for len(out) < n {
		rnd.Shuffle(len(ws), func(i, j int) { ws[i], ws[j] = ws[j], ws[i] })
		out = append(out, ws[:min(n-len(out), len(ws))]...)
	}
	return strings.Join(out, " ")
}

// drill returns short groups that repeat the key next to home-row keys, e.g. "%% a% %j".
func drill(k string, rnd *rand.Rand) string {
	groups := []string{k + k, "a" + k, k + "a", k + k + k, "f" + k, k + "j", "d" + k + "k", k + "l" + k}
	rnd.Shuffle(len(groups), func(i, j int) { groups[i], groups[j] = groups[j], groups[i] })
	return strings.Join(groups[:6], " ")
}
