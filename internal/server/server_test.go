package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/guettli/vibetype/internal/lessons"
	"github.com/guettli/vibetype/internal/store"
	"github.com/guettli/vibetype/web"
)

type env struct {
	srv *Server
	t   *testing.T
	ts  *httptest.Server
	st  *store.Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv, err := New(Config{RPID: "localhost", Origin: "http://localhost:8080", Static: web.Static(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Store: st})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return &env{srv: srv, t: t, ts: ts, st: st}
}

// login creates a user (bypassing WebAuthn) and returns its session token.
func (e *env) login(name string) (store.User, string) {
	e.t.Helper()
	u := store.User{ID: store.NewID(), DisplayName: name}
	if err := e.st.CreateUserWithPasskey(context.Background(), u, store.Passkey{ID: store.NewID(), PublicKey: []byte{1}, Credential: []byte("{}")}); err != nil {
		e.t.Fatal(err)
	}
	tok, err := e.st.CreateAuthSession(context.Background(), u.ID, time.Hour)
	if err != nil {
		e.t.Fatal(err)
	}
	return u, tok
}

func (e *env) do(token, method, path string, body any, out any) int {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, rd)
	req.Header.Set("Origin", "http://localhost:8080")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			e.t.Fatalf("%s %s: decode: %v", method, path, err)
		}
	}
	return res.StatusCode
}

func TestTextsAndSync(t *testing.T) {
	e := newEnv(t)
	alice, aTok := e.login("Alice")
	bob, bTok := e.login("Bob")

	if code := e.do("", "GET", "/api/texts", nil, nil); code != 401 {
		t.Fatalf("anonymous list: %d", code)
	}

	var txt store.Text
	if code := e.do(aTok, "POST", "/api/texts", map[string]string{"title": " Übung  1 ", "content": "  Hallo\r\n\r\nWelt  "}, &txt); code != 201 {
		t.Fatalf("create: %d", code)
	}
	if txt.Title != "Übung 1" || txt.Content != "Hallo\nWelt" || len(txt.ID) != 6 || txt.CreatorID != alice.ID {
		t.Fatalf("unexpected text %+v", txt)
	}
	if code := e.do(aTok, "POST", "/api/texts", map[string]string{"title": "x", "content": "   "}, nil); code != 400 {
		t.Fatalf("empty content: %d", code)
	}

	var got store.Text
	if code := e.do(bTok, "GET", "/api/texts/"+txt.ID, nil, &got); code != 200 || got.CreatorName != "Alice" {
		t.Fatalf("bob get text: %d %+v", code, got)
	}

	sid := store.NewID()
	start := time.Now().UTC().Add(-time.Minute)
	sync := func(tok string, cursor, errs int, finished *time.Time) int {
		return e.do(tok, "PUT", "/api/sessions/"+sid, map[string]any{
			"text_id": txt.ID, "cursor_pos": cursor, "error_count": errs, "cpm": cursor,
			"started_at": start, "finished_at": finished}, nil)
	}
	if c := sync(bTok, 3, 1, nil); c != 200 {
		t.Fatalf("sync: %d", c)
	}
	if c := sync(bTok, 2, 0, nil); c != 200 { // out-of-order request must not reduce progress
		t.Fatalf("sync older: %d", c)
	}
	if c := sync(aTok, 5, 0, nil); c != 403 {
		t.Fatalf("alice hijacks bob's session: %d", c)
	}
	if c := sync(bTok, 99, 0, nil); c != 400 {
		t.Fatalf("cursor beyond text: %d", c)
	}
	now := time.Now()
	if c := sync(bTok, 4, 1, &now); c != 400 {
		t.Fatalf("finished before end: %d", c)
	}
	if c := sync(bTok, 10, 2, &now); c != 200 { // "Hallo\nWelt" has 10 runes
		t.Fatalf("finish: %d", c)
	}

	if c := e.do(bTok, "GET", "/api/texts/"+txt.ID+"/sessions", nil, nil); c != 403 {
		t.Fatalf("bob live view: %d", c)
	}
	var live struct {
		Sessions []store.TypingSession `json:"sessions"`
	}
	if c := e.do(aTok, "GET", "/api/texts/"+txt.ID+"/sessions", nil, &live); c != 200 {
		t.Fatalf("alice live view: %d", c)
	}
	if len(live.Sessions) != 1 {
		t.Fatalf("sessions: %+v", live.Sessions)
	}
	s := live.Sessions[0]
	if s.UserID != bob.ID || s.UserName != "Bob" || s.CursorPos != 10 || s.ErrorCount != 2 || s.FinishedAt == nil {
		t.Fatalf("unexpected session %+v", s)
	}

	var mine []store.Text
	if c := e.do(aTok, "GET", "/api/texts", nil, &mine); c != 200 || len(mine) != 1 {
		t.Fatalf("alice texts: %d %+v", c, mine)
	}
	if c := e.do(aTok, "POST", "/api/logout", nil, nil); c != 204 {
		t.Fatalf("logout: %d", c)
	}
	if c := e.do(aTok, "GET", "/api/me", nil, nil); c != 401 {
		t.Fatalf("after logout: %d", c)
	}
}

func TestStaticAndSPA(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/", "/t/abc123", "/konto", "/static/app.js", "/static/engine.js", "/static/keyboard.js", "/static/progress.js", "/static/style.css", "/sw.js", "/healthz"} {
		if c := e.do("", "GET", p, nil, nil); c != 200 {
			t.Errorf("GET %s: %d", p, c)
		}
	}
}

func TestRegisterBegin(t *testing.T) {
	e := newEnv(t)
	var opts struct {
		PublicKey struct {
			RP                     struct{ ID string } `json:"rp"`
			AuthenticatorSelection struct {
				ResidentKey string `json:"residentKey"`
			} `json:"authenticatorSelection"`
		} `json:"publicKey"`
	}
	if c := e.do("", "POST", "/api/register/begin", map[string]string{"display_name": "Thomas"}, &opts); c != 200 {
		t.Fatalf("register begin: %d", c)
	}
	if opts.PublicKey.RP.ID != "localhost" || opts.PublicKey.AuthenticatorSelection.ResidentKey != "required" {
		t.Fatalf("unexpected options %+v", opts)
	}
	if c := e.do("", "POST", "/api/register/finish", map[string]string{}, nil); c != 400 {
		t.Fatalf("finish without ceremony: %d", c)
	}
}

func TestHardening(t *testing.T) {
	e := newEnv(t)
	_, tok := e.login("Eve")

	// cross-origin and non-JSON writes are refused
	for _, tc := range []struct {
		origin, ct string
		want       int
	}{
		{"https://evil.thomas-guettler.de", "application/json", 403},
		{"", "application/json", 403},
		{"http://localhost:8080", "text/plain", 415},
	} {
		req, _ := http.NewRequest("POST", e.ts.URL+"/api/texts", bytes.NewReader([]byte(`{"title":"x","content":"y"}`)))
		req.Header.Set("Content-Type", tc.ct)
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: tok})
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.want {
			t.Errorf("origin=%q ct=%q: got %d, want %d", tc.origin, tc.ct, res.StatusCode, tc.want)
		}
	}

	var errResp struct{ Error string }
	if c := e.do(tok, "POST", "/api/texts", map[string]string{"title": "x", "content": "Hallo 😀"}, &errResp); c != 400 {
		t.Fatalf("emoji text: %d", c)
	}
	var txt store.Text
	if c := e.do(tok, "POST", "/api/texts", map[string]string{"title": "x", "content": "abc"}, &txt); c != 201 {
		t.Fatalf("create: %d", c)
	}
	start := time.Now().Add(-time.Minute)
	put := func(id string, body map[string]any) int {
		b := map[string]any{"text_id": txt.ID, "cursor_pos": 1, "error_count": 0, "cpm": 100, "started_at": start}
		for k, v := range body {
			b[k] = v
		}
		return e.do(tok, "PUT", "/api/sessions/"+id, b, nil)
	}
	if c := put("session-absurd", map[string]any{"cpm": 99999}); c != 200 {
		t.Errorf("absurd cpm must be clamped, not rejected: %d", c)
	}
	var clamped struct{ Results []store.Result }
	if e.do(tok, "GET", "/api/me/results", nil, &clamped); len(clamped.Results) != 1 || clamped.Results[0].CPM != maxCPM {
		t.Errorf("clamped cpm: %+v", clamped.Results)
	}
	if c := put(store.NewID(), map[string]any{"cpm": -1}); c != 400 {
		t.Errorf("negative cpm: %d", c)
	}
	if c := put(store.NewID(), map[string]any{"started_at": time.Now().Add(time.Hour)}); c != 400 {
		t.Errorf("future start: %d", c)
	}
	if c := put(store.NewID(), map[string]any{"cursor_pos": 3, "finished_at": start.Add(-time.Hour)}); c != 400 {
		t.Errorf("finished before start: %d", c)
	}
	for i := range maxSessionsPerText - 1 { // one already created above
		if c := put(fmt.Sprintf("session-%03d", i), nil); c != 200 {
			t.Fatalf("session %d: %d", i, c)
		}
	}
	if c := put("session-one-more", nil); c != 200 {
		t.Errorf("session beyond the limit must prune, not fail: %d", c)
	}
	var all struct{ Results []store.Result }
	if e.do(tok, "GET", "/api/me/results", nil, &all); len(all.Results) != maxSessionsPerText {
		t.Errorf("sessions after pruning: %d", len(all.Results))
	}
}

func TestResultsAndDelete(t *testing.T) {
	e := newEnv(t)
	_, aTok := e.login("Alice")
	_, bTok := e.login("Bob")
	var t1, t2 store.Text
	e.do(aTok, "POST", "/api/texts", map[string]string{"title": "Eins", "content": "abc"}, &t1)
	e.do(aTok, "POST", "/api/texts", map[string]string{"title": "Zwei", "content": "äöü ß"}, &t2)
	start := time.Now().UTC().Add(-time.Minute)
	fin := time.Now().UTC()
	for i, tt := range []store.Text{t1, t2, t1} {
		if c := e.do(bTok, "PUT", fmt.Sprintf("/api/sessions/session-%d", i), map[string]any{
			"text_id": tt.ID, "cursor_pos": 3, "error_count": i, "cpm": 100 + i, "started_at": start.Add(time.Duration(i) * time.Second),
			"finished_at": map[bool]any{true: fin, false: nil}[tt.ID == t1.ID]}, nil); c != 200 {
			t.Fatalf("sync %d: %d", i, c)
		}
	}
	var res struct {
		Results []store.Result `json:"results"`
		Best    []store.Best   `json:"best"`
	}
	if c := e.do(bTok, "GET", "/api/me/results", nil, &res); c != 200 || len(res.Results) != 3 {
		t.Fatalf("results: %d %+v", c, res)
	}
	if r := res.Results[0]; r.ID != "session-2" || r.TextTitle != "Eins" || r.TextLength != 3 || r.FinishedAt == nil {
		t.Fatalf("newest result: %+v", r)
	}
	if r := res.Results[1]; r.TextTitle != "Zwei" || r.TextLength != 5 || r.FinishedAt != nil {
		t.Fatalf("utf-8 length / unfinished: %+v", r)
	}
	if len(res.Best) != 1 || res.Best[0].ID != "session-2" || res.Best[0].TextID != t1.ID || res.Best[0].CPM != 102 { // finished on t1 with the higher cpm
		t.Fatalf("best: %+v", res.Best)
	}
	if c := e.do(bTok, "GET", "/api/me/results?text_id="+t1.ID, nil, &res); c != 200 || len(res.Results) != 2 {
		t.Fatalf("filtered: %d %d", c, len(res.Results))
	}
	if c := e.do(aTok, "GET", "/api/me/results", nil, &res); c != 200 || len(res.Results) != 0 {
		t.Fatalf("alice has no results: %d %d", c, len(res.Results))
	}

	if c := e.do(bTok, "DELETE", "/api/texts/"+t1.ID, nil, nil); c != 403 {
		t.Fatalf("bob deletes alice's text: %d", c)
	}
	if c := e.do(aTok, "DELETE", "/api/texts/"+t1.ID, nil, nil); c != 204 {
		t.Fatalf("delete: %d", c)
	}
	if c := e.do(aTok, "DELETE", "/api/texts/"+t1.ID, nil, nil); c != 404 {
		t.Fatalf("delete twice: %d", c)
	}
	if c := e.do(bTok, "GET", "/api/me/results", nil, &res); c != 200 || len(res.Results) != 1 {
		t.Fatalf("sessions of deleted text must be gone: %d %d", c, len(res.Results))
	}
	if c := e.do(bTok, "PUT", "/api/sessions/session-0", map[string]any{"text_id": t1.ID, "cursor_pos": 1, "error_count": 0, "cpm": 1, "started_at": start}, nil); c != 404 {
		t.Fatalf("sync to deleted text: %d", c)
	}
}

func TestLessons(t *testing.T) {
	e := newEnv(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := SeedLessons(context.Background(), e.st, logger); err != nil {
		t.Fatal(err)
	}
	if err := SeedLessons(context.Background(), e.st, logger); err != nil { // idempotent
		t.Fatalf("second seed: %v", err)
	}
	_, tok := e.login("Lena")
	var ls []struct {
		ID, Title string
		Length    int
	}
	if c := e.do(tok, "GET", "/api/lessons", nil, &ls); c != 200 || len(ls) != len(lessons.All) || ls[0].ID != "lek01" || ls[0].Length == 0 {
		t.Fatalf("lessons: %d %+v", c, ls)
	}
	var txt store.Text
	if c := e.do(tok, "GET", "/api/texts/lek01", nil, &txt); c != 200 || txt.CreatorName != lessons.SystemUserName {
		t.Fatalf("lesson text: %d %+v", c, txt)
	}
	var withNext struct {
		NextLesson string `json:"next_lesson"`
	}
	if e.do(tok, "GET", "/api/texts/lek01", nil, &withNext); withNext.NextLesson != "lek02" {
		t.Fatalf("next lesson: %+v", withNext)
	}
	withNext.NextLesson = ""
	if e.do(tok, "GET", "/api/texts/lek13", nil, &withNext); withNext.NextLesson != "" {
		t.Fatalf("last lesson has no next: %+v", withNext)
	}
	if c := e.do(tok, "DELETE", "/api/texts/lek01", nil, nil); c != 403 {
		t.Fatalf("users must not delete lessons: %d", c)
	}
	// changed lesson content resets its sessions; removed lessons disappear
	n := utf8.RuneCountInString(txt.Content)
	if c := e.do(tok, "PUT", "/api/sessions/session-lesson", map[string]any{"text_id": "lek01", "cursor_pos": n, "error_count": 0, "cpm": 100,
		"started_at": time.Now().Add(-time.Minute)}, nil); c != 200 {
		t.Fatalf("sync: %d", c)
	}
	if deleted, err := e.st.SeedTexts(context.Background(), lessons.SystemUserID, lessons.SystemUserName, []store.SeedText{{ID: "lek01", Title: "x", Content: "asdf"}}); err != nil || deleted != 1 {
		t.Fatalf("reseed: %d %v", deleted, err)
	}
	var res struct{ Results []store.Result }
	if c := e.do(tok, "GET", "/api/me/results?text_id=lek01", nil, &res); c != 200 || len(res.Results) != 0 {
		t.Fatalf("sessions of changed lesson: %d %+v", c, res.Results)
	}
	if c := e.do(tok, "GET", "/api/texts/lek02", nil, nil); c != 404 {
		t.Fatalf("lesson removed from the course must be gone: %d", c)
	}
	// a user text can never be overwritten by the seeder
	var mine store.Text
	e.do(tok, "POST", "/api/texts", map[string]string{"title": "t", "content": "abc"}, &mine)
	if _, err := e.st.SeedTexts(context.Background(), lessons.SystemUserID, lessons.SystemUserName, []store.SeedText{{ID: mine.ID, Title: "x", Content: "y"}}); err == nil {
		t.Fatal("seeding over a user text must fail")
	}
}

func TestReservedName(t *testing.T) {
	e := newEnv(t)
	if c := e.do("", "POST", "/api/register/begin", map[string]string{"display_name": " VibeType "}, nil); c != 400 {
		t.Fatalf("reserved name: %d", c)
	}
}

func TestWeakKeysAndPractice(t *testing.T) {
	e := newEnv(t)
	_, tok := e.login("Wanda")
	var txt store.Text
	content := "öl öl öl öl öl öl öl öl öl öl asdf asdf asdf"
	if c := e.do(tok, "POST", "/api/texts", map[string]string{"title": "t", "content": content}, &txt); c != 201 {
		t.Fatalf("create: %d", c)
	}
	n := utf8.RuneCountInString(content)
	start := time.Now().Add(-time.Minute)
	put := func(ke map[string]int, errs int) int {
		return e.do(tok, "PUT", "/api/sessions/session-weak", map[string]any{"text_id": txt.ID, "cursor_pos": n, "error_count": errs,
			"cpm": 100, "started_at": start, "key_errors": ke}, nil)
	}
	for _, tc := range []struct {
		ke   map[string]int
		errs int
	}{{map[string]int{"x": 1}, 1}, {map[string]int{"öl": 1}, 1}, {map[string]int{"ö": 0}, 1}, {map[string]int{"ö": 5}, 4}} {
		if c := put(tc.ke, tc.errs); c != 400 {
			t.Errorf("key_errors %v / %d: want 400, got %d", tc.ke, tc.errs, c)
		}
	}
	var conflict struct{ Error string }
	if c := e.do(tok, "POST", "/api/me/practice", nil, &conflict); c != 409 {
		t.Fatalf("practice without data: %d", c)
	}
	if c := put(map[string]int{"ö": 5, "a": 1}, 6); c != 200 {
		t.Fatalf("sync: %d", c)
	}
	var wk struct {
		Keys []store.KeyStat `json:"keys"`
	}
	if c := e.do(tok, "GET", "/api/me/weak-keys", nil, &wk); c != 200 {
		t.Fatalf("weak keys: %d", c)
	}
	// ö: 10 correct + 5 errors = 15 attempts; a: 3 correct + 1 error = 4 attempts (< 10, not rated)
	if len(wk.Keys) != 1 || wk.Keys[0].Char != "ö" || wk.Keys[0].Errors != 5 || wk.Keys[0].Attempts != 15 {
		t.Fatalf("weak keys: %+v", wk.Keys)
	}
	var p store.Text
	if c := e.do(tok, "POST", "/api/me/practice", nil, &p); c != 201 || p.Title != "Übung: ö" || !strings.Contains(p.Content, "ö") {
		t.Fatalf("practice: %d %+v", c, p)
	}
	// a huge count cannot overflow the sum check
	if c := put(map[string]int{"ö": 9223372036854775807, "a": 1}, 6); c != 400 {
		t.Fatalf("overflowing key_errors: %d", c)
	}
	// repeated practice clicks keep only the newest 3 practice texts; user texts are untouched
	for range 3 {
		if c := e.do(tok, "POST", "/api/me/practice", nil, nil); c != 201 {
			t.Fatalf("practice: %d", c)
		}
	}
	var mine []store.Text
	e.do(tok, "GET", "/api/texts", nil, &mine)
	practices := 0
	for _, m := range mine {
		if strings.HasPrefix(m.Title, "Übung: ") {
			practices++
		}
	}
	if practices != 3 || len(mine) != 4 {
		t.Fatalf("after 4 practice clicks: %d practice texts, %d texts", practices, len(mine))
	}
	// a session with errors but without key_errors (old client) must not dilute the rate
	if c := e.do(tok, "PUT", "/api/sessions/session-no-keys", map[string]any{"text_id": txt.ID, "cursor_pos": n, "error_count": 3,
		"cpm": 10, "started_at": start}, nil); c != 200 {
		t.Fatalf("old-style sync: %d", c)
	}
	if e.do(tok, "GET", "/api/me/weak-keys", nil, &wk); len(wk.Keys) != 1 || wk.Keys[0].Attempts != 15 {
		t.Fatalf("weak keys diluted by a session without key data: %+v", wk.Keys)
	}
	// an older client without key_errors still syncs
	if c := e.do(tok, "PUT", "/api/sessions/session-old-client", map[string]any{"text_id": txt.ID, "cursor_pos": 1, "error_count": 0,
		"cpm": 10, "started_at": start}, nil); c != 200 {
		t.Fatalf("old client: %d", c)
	}
}

func TestQR(t *testing.T) {
	e := newEnv(t)
	_, tok := e.login("Quinn")
	var txt store.Text
	e.do(tok, "POST", "/api/texts", map[string]string{"title": "t", "content": "abc"}, &txt)
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/texts/"+txt.ID+"/qr.svg", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: tok})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/svg+xml" || !strings.HasPrefix(string(body), "<svg") || !strings.Contains(string(body), "M4 4h1v1h-1z") {
		t.Fatalf("qr: %d %s %.80s", res.StatusCode, res.Header.Get("Content-Type"), body)
	}
	if c := e.do(tok, "GET", "/api/texts/nope42/qr.svg", nil, nil); c != 404 {
		t.Fatalf("qr of unknown text: %d", c)
	}
}

func TestAccount(t *testing.T) {
	e := newEnv(t)
	u, tok := e.login("Paula")
	var me store.User
	if c := e.do(tok, "PUT", "/api/me", map[string]string{"display_name": "  Paula   K. "}, &me); c != 200 || me.DisplayName != "Paula K." {
		t.Fatalf("rename: %d %+v", c, me)
	}
	if c := e.do(tok, "PUT", "/api/me", map[string]string{"display_name": "vibetype"}, nil); c != 400 {
		t.Fatalf("reserved rename: %d", c)
	}
	var pks []store.PasskeyInfo
	if c := e.do(tok, "GET", "/api/me/passkeys", nil, &pks); c != 200 || len(pks) != 1 {
		t.Fatalf("passkeys: %d %+v", c, pks)
	}
	if c := e.do(tok, "DELETE", "/api/me/passkeys/"+pks[0].ID, nil, nil); c != 409 {
		t.Fatalf("deleting the last passkey must be refused: %d", c)
	}
	if err := e.st.AddPasskey(context.Background(), store.Passkey{ID: "second", UserID: u.ID, PublicKey: []byte{1}, Credential: []byte("{}")}, "Handy"); err != nil {
		t.Fatal(err)
	}
	_, otherTok := e.login("Otto")
	if c := e.do(otherTok, "DELETE", "/api/me/passkeys/second", nil, nil); c != 404 {
		t.Fatalf("deleting someone else's passkey: %d", c)
	}
	if c := e.do(tok, "DELETE", "/api/me/passkeys/"+pks[0].ID, nil, nil); c != 204 {
		t.Fatalf("delete: %d", c)
	}
	if e.do(tok, "GET", "/api/me/passkeys", nil, &pks); len(pks) != 1 || pks[0].Label != "Handy" {
		t.Fatalf("after delete: %+v", pks)
	}
	var opts struct {
		PublicKey struct {
			ExcludeCredentials []any `json:"excludeCredentials"`
		} `json:"publicKey"`
	}
	if c := e.do(tok, "POST", "/api/me/passkeys/begin", map[string]string{"label": "Laptop"}, &opts); c != 200 || len(opts.PublicKey.ExcludeCredentials) != 1 {
		t.Fatalf("add begin: %d %+v", c, opts)
	}
	// the add ceremony cannot be used to sign up a new account
	if c := e.do("", "POST", "/api/register/finish", map[string]string{}, nil); c != 400 {
		t.Fatalf("finish without ceremony: %d", c)
	}
}

func TestPasskeyChangesNeedRecentLogin(t *testing.T) {
	e := newEnv(t)
	u, tok := e.login("Rita")
	if err := e.st.AddPasskey(context.Background(), store.Passkey{ID: "second", UserID: u.ID, PublicKey: []byte{1}, Credential: []byte("{}")}, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.st.AddPasskey(context.Background(), store.Passkey{ID: "second", UserID: u.ID, PublicKey: []byte{1}, Credential: []byte("{}")}, ""); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("duplicate credential: %v", err)
	}
	otherTok, err := e.st.CreateAuthSession(context.Background(), u.ID, time.Hour) // e.g. a stolen or forgotten session
	if err != nil {
		t.Fatal(err)
	}
	e.srv.now = func() time.Time { return time.Now().Add(reauthWindow + time.Minute) }
	var resp struct{ Error, Code string }
	if c := e.do(tok, "DELETE", "/api/me/passkeys/second", nil, &resp); c != 403 || resp.Code != "reauth_required" {
		t.Fatalf("stale login delete: %d %+v", c, resp)
	}
	if c := e.do(tok, "POST", "/api/me/passkeys/begin", map[string]string{"label": "x"}, &resp); c != 403 || resp.Code != "reauth_required" {
		t.Fatalf("stale login add: %d %+v", c, resp)
	}
	if c := e.do(tok, "PUT", "/api/me", map[string]string{"display_name": "Rita R."}, nil); c != 200 {
		t.Fatalf("rename needs no fresh login: %d", c)
	}
	e.srv.now = time.Now
	if c := e.do(tok, "DELETE", "/api/me/passkeys/second", nil, nil); c != 204 {
		t.Fatalf("fresh login delete: %d", c)
	}
	if c := e.do(otherTok, "GET", "/api/me", nil, nil); c != 401 {
		t.Fatalf("other sessions must end when a passkey is deleted: %d", c)
	}
	if c := e.do(tok, "GET", "/api/me", nil, nil); c != 200 {
		t.Fatalf("own session must survive: %d", c)
	}
}

func TestActiveMS(t *testing.T) {
	e := newEnv(t)
	_, tok := e.login("Ali")
	var txt store.Text
	e.do(tok, "POST", "/api/texts", map[string]string{"title": "t", "content": "abc"}, &txt)
	start := time.Now().UTC().Add(-time.Minute)
	fin := time.Now().UTC()
	put := func(id string, cpm int, active any) int {
		return e.do(tok, "PUT", "/api/sessions/"+id, map[string]any{"text_id": txt.ID, "cursor_pos": 3, "error_count": 0,
			"cpm": cpm, "started_at": start, "finished_at": fin, "active_ms": active}, nil)
	}
	if c := put("session-too-long", 10, 10*60*1000); c != 400 {
		t.Fatalf("active_ms beyond elapsed time: %d", c)
	}
	if c := put("session-negative", 10, -1); c != 400 {
		t.Fatalf("negative active_ms: %d", c)
	}
	// an old wall-time run with a higher cpm, then a pause-aware run with a lower one
	if c := put("session-old", 500, nil); c != 200 {
		t.Fatalf("old run: %d", c)
	}
	if c := put("session-new", 300, 20000); c != 200 {
		t.Fatalf("new run: %d", c)
	}
	var res struct {
		Results []store.Result `json:"results"`
		Best    []store.Best   `json:"best"`
	}
	e.do(tok, "GET", "/api/me/results", nil, &res)
	if len(res.Best) != 1 || res.Best[0].ID != "session-new" {
		t.Fatalf("pause-aware run must be the best (not comparable with the old one): %+v", res.Best)
	}
	for _, r := range res.Results {
		if (r.ID == "session-new") != (r.ActiveMS != nil) {
			t.Fatalf("active_ms not returned correctly: %+v", r)
		}
	}
}

func TestProgress(t *testing.T) {
	e := newEnv(t)
	_, tok := e.login("Paula")
	_, other := e.login("Otto")
	var txt store.Text
	if c := e.do(tok, "POST", "/api/texts", map[string]string{"title": "t", "content": "abc def"}, &txt); c != 201 {
		t.Fatalf("create: %d", c)
	}
	start := time.Now().Add(-time.Minute)
	fin := time.Now()
	sync := func(tok, id string, cursor int, finished *time.Time) {
		t.Helper()
		body := map[string]any{"text_id": txt.ID, "cursor_pos": cursor, "error_count": 1, "cpm": 120, "started_at": start,
			"finished_at": finished, "active_ms": 30000}
		if c := e.do(tok, "PUT", "/api/sessions/"+id, body, nil); c != 200 {
			t.Fatalf("sync %s: %d", id, c)
		}
	}
	sync(tok, "progress-finished", 7, &fin)
	sync(tok, "progress-running", 3, nil)
	sync(tok, "progress-untouched", 0, nil) // opened but nothing typed: not practice
	sync(other, "progress-other", 7, &fin)  // someone else's run

	type resp struct {
		Runs      []store.Activity `json:"runs"`
		Days      int              `json:"days"`
		Limit     int              `json:"limit"`
		Truncated bool             `json:"truncated"`
	}
	var p resp
	if c := e.do(tok, "GET", "/api/me/progress", nil, &p); c != 200 {
		t.Fatalf("progress: %d", c)
	}
	if p.Days != 90 || p.Limit != progressLimit || p.Truncated || len(p.Runs) != 2 {
		t.Fatalf("progress: %+v", p)
	}
	if r := p.Runs[1]; r.FinishedAt == nil || r.CursorPos != 7 || r.ErrorCount != 1 || r.CPM != 120 || r.ActiveMS == nil || *r.ActiveMS != 30000 {
		t.Fatalf("finished run: %+v", r)
	}
	if r := p.Runs[0]; r.FinishedAt != nil || r.CursorPos != 3 {
		t.Fatalf("running run (newest first): %+v", r)
	}
	// 91 days later both runs are outside the window
	e.srv.now = func() time.Time { return time.Now().Add(91 * 24 * time.Hour) }
	if c := e.do(tok, "GET", "/api/me/progress", nil, &p); c != 200 || len(p.Runs) != 0 {
		t.Fatalf("progress after 91 days: %d %+v", c, p)
	}
	if c := e.do("", "GET", "/api/me/progress", nil, nil); c != 401 {
		t.Fatalf("progress without login: %d", c)
	}
}
