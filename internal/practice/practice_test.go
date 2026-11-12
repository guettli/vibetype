package practice

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/guettli/vibetype/internal/textnorm"
)

func check(t *testing.T, got string) {
	t.Helper()
	if got == "" || textnorm.Normalize(got) != got || len(textnorm.Untypable(got)) > 0 {
		t.Fatalf("practice text empty, not normalized or not typable:\n%q", got)
	}
}

func TestText(t *testing.T) {
	rnd := rand.New(rand.NewPCG(1, 2))
	got := Text([]string{"z", "%"}, rnd)
	check(t, got)
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("want 2 lines per key:\n%s", got)
	}
	for _, i := range []int{0, 1} {
		ws := strings.Fields(lines[i])
		if len(ws) != wordsPerLine {
			t.Errorf("line %d: %d words", i, len(ws))
		}
		for _, w := range ws {
			if !strings.Contains(w, "z") {
				t.Errorf("word %q lacks z", w)
			}
		}
	}
	if !strings.Contains(lines[2], "%") {
		t.Errorf("drill for %%: %q", lines[2])
	}
	if Text(nil, rnd) != "" {
		t.Fatal("no keys, no text")
	}
}

// Space and Enter are the most frequent characters and can be weak keys too.
func TestSpaceAndEnter(t *testing.T) {
	rnd := rand.New(rand.NewPCG(5, 6))
	sp := Text([]string{" "}, rnd)
	check(t, sp)
	if strings.Count(sp, " ") < 2*wordsPerLine {
		t.Errorf("space drill has few spaces: %q", sp)
	}
	en := Text([]string{"\n"}, rnd)
	check(t, en)
	if strings.Count(en, "\n") < 5 {
		t.Errorf("enter drill has few lines: %q", en)
	}
}

func TestRareKeyMixesWordsAndDrills(t *testing.T) {
	got := Text([]string{"@"}, rand.New(rand.NewPCG(7, 8)))
	check(t, got)
	if strings.Count(got, "info@example.org") > 2 {
		t.Errorf("single word repeated: %q", got)
	}
}

func TestPoolHasNoDrills(t *testing.T) {
	for _, w := range wordPool {
		switch w {
		case "fj", "dk", "asdf", "jklö", "öü", "Öö", "öp", "fff", "gag", "Aa":
			t.Errorf("drill fragment %q in the word pool", w)
		}
	}
	if len(wordPool) < 200 {
		t.Errorf("word pool suspiciously small: %d", len(wordPool))
	}
}
