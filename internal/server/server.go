// Package server contains the HTTP handlers of vibetype.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/guettli/vibetype/internal/lessons"
	"github.com/guettli/vibetype/internal/practice"
	"github.com/guettli/vibetype/internal/store"
	"github.com/guettli/vibetype/internal/textnorm"
)

const (
	sessionCookie  = "vt_session"
	ceremonyCookie = "vt_ceremony"
	sessionTTL     = 180 * 24 * time.Hour
	ceremonyTTL    = 5 * time.Minute

	maxTitleLen   = 200
	maxContentLen = 20000
	maxNameLen    = 64

	maxResults           = 200
	maxCPM               = 3000 // world records are around 1000-1300 cpm
	maxSessionsPerText   = 50   // per user and text
	maxPendingCeremonies = 10000
	maxWebAuthnBodyBytes = 64 << 10
)

// Config configures the server.
type Config struct {
	Version string // shown by /healthz
	RPID    string // e.g. vibetype.thomas-guettler.de
	Origin  string // e.g. https://vibetype.thomas-guettler.de
	Static  fs.FS  // frontend assets (index.html, app.js, ...)
	Logger  *slog.Logger
	Store   *store.Store
}

// Server implements http.Handler.
type Server struct {
	cfg    Config
	wa     *webauthn.WebAuthn
	mux    *http.ServeMux
	secure bool

	mu         sync.Mutex
	ceremonies map[string]ceremony // key: random id in ceremonyCookie
}

type ceremony struct {
	data    webauthn.SessionData
	user    *waUser // only set during registration
	expires time.Time
}

// New builds the server.
func New(cfg Config) (*Server, error) {
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.RPID,
		RPDisplayName: "vibetype",
		RPOrigins:     []string{cfg.Origin},
	})
	if err != nil {
		return nil, fmt.Errorf("webauthn config: %w", err)
	}
	s := &Server{
		cfg:        cfg,
		wa:         wa,
		mux:        http.NewServeMux(),
		secure:     strings.HasPrefix(cfg.Origin, "https://"),
		ceremonies: map[string]ceremony{},
	}
	s.routes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CSRF protection: every state-changing request must come from our own origin.
	// SameSite=Lax alone is not enough, sibling subdomains count as same-site.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if origin := r.Header.Get("Origin"); origin != s.cfg.Origin {
			httpError(w, http.StatusForbidden, fmt.Sprintf("cross-origin request refused (Origin %q)", origin))
			return
		}
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; frame-ancestors 'none'")
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, "ok %s\n", s.cfg.Version) })

	m.HandleFunc("POST /api/register/begin", s.registerBegin)
	m.HandleFunc("POST /api/register/finish", s.registerFinish)
	m.HandleFunc("POST /api/login/begin", s.loginBegin)
	m.HandleFunc("POST /api/login/finish", s.loginFinish)
	m.HandleFunc("POST /api/logout", s.logout)

	m.HandleFunc("GET /api/me", s.auth(s.me))
	m.HandleFunc("GET /api/texts", s.auth(s.listTexts))
	m.HandleFunc("POST /api/texts", s.auth(s.createText))
	m.HandleFunc("GET /api/texts/{id}", s.auth(s.getText))
	m.HandleFunc("GET /api/texts/{id}/sessions", s.auth(s.liveSessions))
	m.HandleFunc("DELETE /api/texts/{id}", s.auth(s.deleteText))
	m.HandleFunc("GET /api/me/results", s.auth(s.myResults))
	m.HandleFunc("GET /api/lessons", s.auth(s.listLessons))
	m.HandleFunc("GET /api/me/weak-keys", s.auth(s.weakKeys))
	m.HandleFunc("POST /api/me/practice", s.auth(s.createPractice))
	m.HandleFunc("PUT /api/sessions/{id}", s.auth(s.syncSession))

	static := http.FileServerFS(s.cfg.Static)
	m.Handle("GET /static/", http.StripPrefix("/static/", static))
	m.HandleFunc("GET /sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, s.cfg.Static, "sw.js")
	})
	index := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, s.cfg.Static, "index.html")
	}
	m.HandleFunc("GET /{$}", index)
	m.HandleFunc("GET /t/{id}", index)
}

// ---- helpers ----

type ctxKey struct{}

func userFrom(ctx context.Context) store.User { return ctx.Value(ctxKey{}).(store.User) }

func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			httpError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		u, err := s.cfg.Store.UserForToken(r.Context(), c.Value)
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusUnauthorized, "session expired")
			return
		}
		if err != nil {
			s.internal(w, r, err)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

func httpError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.cfg.Logger.Error("internal error", "method", r.Method, "path", r.URL.Path, "err", err)
	httpError(w, http.StatusInternalServerError, "internal error")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		httpError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func (s *Server) setCookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ---- texts ----

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userFrom(r.Context()))
}

func (s *Server) listTexts(w http.ResponseWriter, r *http.Request) {
	texts, err := s.cfg.Store.TextsByCreator(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, texts)
}

func (s *Server) createText(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	title := strings.Join(strings.Fields(in.Title), " ")
	content := textnorm.Normalize(in.Content)
	switch {
	case title == "":
		httpError(w, http.StatusBadRequest, "title is empty")
		return
	case utf8.RuneCountInString(title) > maxTitleLen:
		httpError(w, http.StatusBadRequest, fmt.Sprintf("title longer than %d characters", maxTitleLen))
		return
	case content == "":
		httpError(w, http.StatusBadRequest, "content is empty")
		return
	case utf8.RuneCountInString(content) > maxContentLen:
		httpError(w, http.StatusBadRequest, fmt.Sprintf("content longer than %d characters", maxContentLen))
		return
	}
	if bad := textnorm.Untypable(content); len(bad) > 0 {
		httpError(w, http.StatusBadRequest, fmt.Sprintf("content contains characters that cannot be typed on a keyboard: %q", string(bad)))
		return
	}
	u := userFrom(r.Context())
	t, err := s.cfg.Store.CreateText(r.Context(), u.ID, title, content)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	t.CreatorName = u.DisplayName
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) getText(w http.ResponseWriter, r *http.Request) {
	t, err := s.cfg.Store.GetText(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "text not found")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	type textWithNext struct {
		store.Text
		NextLesson string `json:"next_lesson,omitempty"`
	}
	writeJSON(w, http.StatusOK, textWithNext{Text: t, NextLesson: lessons.Next(t.ID)})
}

// liveSessions returns all typing sessions of a text. Only the creator of the
// text gets the live view.
func (s *Server) liveSessions(w http.ResponseWriter, r *http.Request) {
	t, err := s.cfg.Store.GetText(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "text not found")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if t.CreatorID != userFrom(r.Context()).ID {
		httpError(w, http.StatusForbidden, "only the creator of a text sees the live view")
		return
	}
	sessions, err := s.cfg.Store.TypingSessionsForText(r.Context(), t.ID, 500)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"server_time": time.Now().UTC(), "sessions": sessions})
}

// syncSession is called by the client sync loop every few seconds.
func (s *Server) syncSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TextID     string         `json:"text_id"`
		CursorPos  int            `json:"cursor_pos"`
		ErrorCount int            `json:"error_count"`
		CPM        int            `json:"cpm"`
		StartedAt  time.Time      `json:"started_at"`
		FinishedAt *time.Time     `json:"finished_at"`
		KeyErrors  map[string]int `json:"key_errors"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	id := r.PathValue("id")
	if len(id) < 8 || len(id) > 64 {
		httpError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	t, err := s.cfg.Store.GetText(r.Context(), in.TextID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "text not found")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	n := utf8.RuneCountInString(t.Content)
	now := time.Now()
	switch {
	case in.CursorPos < 0 || in.CursorPos > n || in.ErrorCount < 0:
		httpError(w, http.StatusBadRequest, "invalid progress values")
		return
	case in.CPM < 0:
		httpError(w, http.StatusBadRequest, "cpm must not be negative")
		return
	case in.StartedAt.IsZero() || in.StartedAt.After(now.Add(time.Minute)):
		httpError(w, http.StatusBadRequest, "started_at missing or in the future")
		return
	case in.FinishedAt != nil && in.CursorPos != n:
		httpError(w, http.StatusBadRequest, "finished_at set but text not completed")
		return
	case in.FinishedAt != nil && (in.FinishedAt.Before(in.StartedAt) || in.FinishedAt.After(now.Add(time.Minute))):
		httpError(w, http.StatusBadRequest, "finished_at before started_at or in the future")
		return
	}
	if msg := validKeyErrors(in.KeyErrors, t.Content, in.ErrorCount); msg != "" {
		httpError(w, http.StatusBadRequest, "invalid key_errors: "+msg)
		return
	}
	if in.CPM > maxCPM {
		// Not humanly possible (pasted keystrokes, broken clock, cheating). Clamp
		// instead of rejecting so the progress itself is not lost; log it.
		s.cfg.Logger.Warn("implausible cpm clamped", "session", id, "user", userFrom(r.Context()).ID, "cpm", in.CPM, "max", maxCPM)
		in.CPM = maxCPM
	}
	ts, err := s.cfg.Store.UpsertTypingSession(r.Context(), store.TypingSession{
		ID: id, TextID: t.ID, UserID: userFrom(r.Context()).ID,
		CursorPos: in.CursorPos, ErrorCount: in.ErrorCount, CPM: in.CPM, KeyErrors: in.KeyErrors,
		StartedAt: in.StartedAt.UTC(), FinishedAt: utcPtr(in.FinishedAt),
	}, maxSessionsPerText)
	if errors.Is(err, store.ErrNotFound) { // deleted concurrently
		httpError(w, http.StatusNotFound, "text not found")
		return
	}
	if errors.Is(err, store.ErrForbidden) {
		httpError(w, http.StatusForbidden, "session belongs to another user or text")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": ts.ID, "updated_at": ts.UpdatedAt})
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// RunJanitor periodically drops expired passkey ceremonies and login sessions
// until ctx is done.
func (s *Server) RunJanitor(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.sweepCeremonies()
		n, err := s.cfg.Store.DeleteExpiredAuthSessions(ctx)
		if err != nil && ctx.Err() == nil {
			s.cfg.Logger.Error("delete expired login sessions", "err", err)
		} else if n > 0 {
			s.cfg.Logger.Info("deleted expired login sessions", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// deleteText removes one of the caller's texts including all typing sessions on it.
func (s *Server) deleteText(w http.ResponseWriter, r *http.Request) {
	err := s.cfg.Store.DeleteText(r.Context(), r.PathValue("id"), userFrom(r.Context()).ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "text not found")
	case errors.Is(err, store.ErrForbidden):
		httpError(w, http.StatusForbidden, "only the creator can delete a text")
	case err != nil:
		s.internal(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// myResults lists the caller's typing sessions (newest first), optionally for one text.
func (s *Server) myResults(w http.ResponseWriter, r *http.Request) {
	uid := userFrom(r.Context()).ID
	results, err := s.cfg.Store.ResultsForUser(r.Context(), uid, r.URL.Query().Get("text_id"), maxResults)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	best, err := s.cfg.Store.BestResults(r.Context(), uid)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "best": best, "limit": maxResults})
}

// listLessons returns the built-in lessons in course order (without content).
func (s *Server) listLessons(w http.ResponseWriter, r *http.Request) {
	texts, err := s.cfg.Store.TextsByCreator(r.Context(), lessons.SystemUserID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	type lesson struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Length int    `json:"length"`
	}
	out := make([]lesson, 0, len(texts))
	for _, t := range texts {
		out = append(out, lesson{ID: t.ID, Title: t.Title, Length: utf8.RuneCountInString(t.Content)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeJSON(w, http.StatusOK, out)
}

// SeedLessons stores the built-in lessons (idempotent, run at startup).
func SeedLessons(ctx context.Context, st *store.Store, logger *slog.Logger) error {
	seed := make([]store.SeedText, 0, len(lessons.All))
	for _, l := range lessons.All {
		seed = append(seed, store.SeedText{ID: l.ID, Title: l.Title, Content: l.Content})
	}
	deleted, err := st.SeedTexts(ctx, lessons.SystemUserID, lessons.SystemUserName, seed)
	if deleted > 0 {
		logger.Warn("lessons changed: deleted typing sessions that referred to the old content", "sessions", deleted)
	}
	return err
}

// validKeyErrors checks the per-character error counts of a sync request; it
// returns "" if they are fine. (Older clients send none, which is fine too.)
func validKeyErrors(ke map[string]int, content string, errorCount int) string {
	if len(ke) > 200 {
		return "too many entries"
	}
	sum := 0
	for k, n := range ke {
		if utf8.RuneCountInString(k) != 1 || !strings.Contains(content, k) {
			return fmt.Sprintf("%q is not a character of the text", k)
		}
		if n < 1 || n > errorCount { // the upper bound also keeps the sum from overflowing
			return fmt.Sprintf("count for %q must be between 1 and error_count", k)
		}
		sum += n
	}
	if sum > errorCount {
		return fmt.Sprintf("sum %d exceeds error_count %d", sum, errorCount)
	}
	return ""
}

const (
	weakKeysWindow      = 30 * 24 * time.Hour
	weakKeysSessions    = 200
	weakKeysMinAttempts = 10
	weakKeysLimit       = 5
	keepPracticeTexts   = 3 // older generated practice texts of a user are deleted
)

func (s *Server) loadWeakKeys(r *http.Request) ([]store.KeyStat, error) {
	return s.cfg.Store.WeakKeys(r.Context(), userFrom(r.Context()).ID, time.Now().Add(-weakKeysWindow),
		weakKeysSessions, weakKeysMinAttempts, weakKeysLimit)
}

// weakKeys returns the caller's characters with the highest error rate.
func (s *Server) weakKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.loadWeakKeys(r)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys, "min_attempts": weakKeysMinAttempts, "days": int(weakKeysWindow.Hours() / 24)})
}

// createPractice creates a personal text that drills the caller's weak keys.
func (s *Server) createPractice(w http.ResponseWriter, r *http.Request) {
	keys, err := s.loadWeakKeys(r)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	chars := make([]string, 0, len(keys))
	for _, k := range keys {
		chars = append(chars, k.Char)
	}
	content := textnorm.Normalize(practice.Text(chars, rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))))
	if content == "" {
		httpError(w, http.StatusConflict, "not enough typing data yet to find weak keys")
		return
	}
	var shown []string
	for _, c := range chars {
		if c == " " {
			c = "Leertaste"
		} else if c == "\n" {
			c = "Enter"
		}
		shown = append(shown, c)
	}
	u := userFrom(r.Context())
	t, err := s.cfg.Store.CreatePracticeText(r.Context(), u.ID, "Übung: "+strings.Join(shown, " "), content, keepPracticeTexts)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	t.CreatorName = u.DisplayName
	writeJSON(w, http.StatusCreated, t)
}
