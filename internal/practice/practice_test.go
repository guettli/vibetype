package practice

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/guettli/vibetype/internal/textnorm"
)

func TestText(t *testing.T) {
	got := Text([]string{"ö", "%", " ", "ß"}, rand.New(rand.NewPCG(1, 2)))
	lines := strings.Split(got, "\n")
	if len(lines) != 6 {
		t.Fatalf("want 2 lines per non-space key, got %d:\n%s", len(lines), got)
	}
	for i, k := range []string{"ö", "ö", "%", "%", "ß", "ß"} {
		if k != "%" && len(strings.Fields(lines[i])) != wordsPerLine {
			t.Errorf("line %d has %d words, want %d: %q", i, len(strings.Fields(lines[i])), wordsPerLine, lines[i])
		}
		for _, w := range strings.Fields(lines[i]) {
			if !strings.Contains(w, k) {
				t.Errorf("line %d word %q lacks %q", i, w, k)
			}
		}
	}
	if textnorm.Normalize(got) != got || len(textnorm.Untypable(got)) > 0 {
		t.Fatalf("practice text not normalized/typable:\n%q", got)
	}
	if Text(nil, rand.New(rand.NewPCG(1, 2))) != "" {
		t.Fatal("no keys, no text")
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
