package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/guettli/vibetype/internal/lessons"
	"github.com/guettli/vibetype/internal/store"
	"github.com/guettli/vibetype/web"
)

type env struct {
	t  *testing.T
	ts *httptest.Server
	st *store.Store
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
	return &env{t: t, ts: ts, st: st}
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
	for _, p := range []string{"/", "/t/abc123", "/static/app.js", "/static/engine.js", "/static/keyboard.js", "/static/style.css", "/sw.js", "/healthz"} {
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
	if c := put("session-too-many", nil); c != 429 {
		t.Errorf("session limit: %d", c)
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
	if err := SeedLessons(context.Background(), e.st); err != nil {
		t.Fatal(err)
	}
	if err := SeedLessons(context.Background(), e.st); err != nil { // idempotent
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
	if e.do(tok, "GET", "/api/texts/lek12", nil, &withNext); withNext.NextLesson != "" {
		t.Fatalf("last lesson has no next: %+v", withNext)
	}
	if c := e.do(tok, "DELETE", "/api/texts/lek01", nil, nil); c != 403 {
		t.Fatalf("users must not delete lessons: %d", c)
	}
	// a lesson that got shorter clamps existing cursors
	n := utf8.RuneCountInString(txt.Content)
	if c := e.do(tok, "PUT", "/api/sessions/session-lesson", map[string]any{"text_id": "lek01", "cursor_pos": n, "error_count": 0, "cpm": 100,
		"started_at": time.Now().Add(-time.Minute)}, nil); c != 200 {
		t.Fatalf("sync: %d", c)
	}
	if err := e.st.SeedTexts(context.Background(), lessons.SystemUserID, lessons.SystemUserName, []store.SeedText{{ID: "lek01", Title: "x", Content: "asdf"}}); err != nil {
		t.Fatal(err)
	}
	var res struct{ Results []store.Result }
	if c := e.do(tok, "GET", "/api/me/results?text_id=lek01", nil, &res); c != 200 || len(res.Results) != 1 || res.Results[0].CursorPos != 4 {
		t.Fatalf("clamped cursor: %d %+v", c, res.Results)
	}
	// a user text can never be overwritten by the seeder
	var mine store.Text
	e.do(tok, "POST", "/api/texts", map[string]string{"title": "t", "content": "abc"}, &mine)
	if err := e.st.SeedTexts(context.Background(), lessons.SystemUserID, lessons.SystemUserName, []store.SeedText{{ID: mine.ID, Title: "x", Content: "y"}}); err == nil {
		t.Fatal("seeding over a user text must fail")
	}
}
