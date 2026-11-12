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

func TestUntypable(t *testing.T) {
	if u := Untypable(Normalize("Grüße, 50 € & 20 °C – „ok“ §1")); len(u) != 0 {
		t.Fatalf("unexpected untypable %q", string(u))
	}
	if u := string(Untypable("a😀b日😀")); u != "😀日" {
		t.Fatalf("got %q", u)
	}
}
