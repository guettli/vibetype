package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

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
	for _, p := range []string{"/", "/t/abc123", "/static/app.js", "/static/engine.js", "/static/style.css", "/sw.js", "/healthz"} {
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
