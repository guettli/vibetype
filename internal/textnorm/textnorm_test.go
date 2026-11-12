package textnorm

import "testing"

func TestNormalize(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"  Hallo   Welt  ", "Hallo Welt"},
		{"a\r\n\r\n\r\nb\rc", "a\nb\nc"},
		{"\tTab\tgetrennt", "Tab getrennt"},
		{"„Zitat“ – so…", `"Zitat" - so...`},
		{"ä", "ä"}, // decomposed umlaut -> NFC
		{"x y­z", "x yz"},
	} {
		if got := Normalize(tc.in); got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
