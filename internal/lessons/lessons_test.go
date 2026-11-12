package lessons

import (
	"strings"
	"testing"
	"unicode"

	"github.com/guettli/vibetype/internal/textnorm"
)

// Each lesson may only use keys introduced so far; capitals only after the "⇧" lesson.
func TestLessonsOnlyUseLearnedKeys(t *testing.T) {
	learned := map[rune]bool{'\n': true}
	capitals := false
	ids := map[string]bool{}
	for _, l := range All {
		if ids[l.ID] {
			t.Errorf("duplicate id %s", l.ID)
		}
		ids[l.ID] = true
		for _, r := range l.NewKeys {
			if r == '⇧' {
				capitals = true
				continue
			}
			learned[r] = true
		}
		var bad []string
		for _, r := range l.Content {
			ok := learned[r] || (capitals && unicode.IsUpper(r) && learned[unicode.ToLower(r)])
			if !ok && !strings.Contains(strings.Join(bad, ""), string(r)) {
				bad = append(bad, string(r))
			}
		}
		if len(bad) > 0 {
			t.Errorf("%s uses keys not learned yet: %q", l.ID, strings.Join(bad, ""))
		}
		if got := textnorm.Normalize(l.Content); got != l.Content {
			t.Errorf("%s is not normalized:\n%q\n%q", l.ID, l.Content, got)
		}
		if u := textnorm.Untypable(l.Content); len(u) > 0 {
			t.Errorf("%s has untypable %q", l.ID, string(u))
		}
		for _, r := range strings.ReplaceAll(l.NewKeys, "⇧", "") {
			if !strings.ContainsRune(l.Content, r) && r != ' ' {
				t.Errorf("%s introduces %q but never uses it", l.ID, r)
			}
		}
	}
}
