// Package store is the SQLite persistence layer of vibetype.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // CGO-free SQLite driver
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrForbidden is returned when a row belongs to another user.
var ErrForbidden = errors.New("forbidden")

// ErrLimit is returned when a per-user quota is exhausted.
var ErrLimit = errors.New("limit reached")

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id           TEXT PRIMARY KEY,
	display_name TEXT NOT NULL,
	created_at   DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS passkeys (
	id         TEXT PRIMARY KEY,               -- base64url credential ID
	user_id    TEXT NOT NULL REFERENCES users(id),
	public_key BLOB NOT NULL,
	sign_count INTEGER NOT NULL,
	credential BLOB NOT NULL,                  -- full webauthn.Credential as JSON (flags, transports, ...)
	created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS passkeys_user ON passkeys(user_id);
CREATE TABLE IF NOT EXISTS texts (
	id         TEXT PRIMARY KEY,
	creator_id TEXT NOT NULL REFERENCES users(id),
	title      TEXT NOT NULL,
	content    TEXT NOT NULL,
	created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS texts_creator ON texts(creator_id, created_at);
CREATE TABLE IF NOT EXISTS typing_sessions (
	id          TEXT PRIMARY KEY,
	text_id     TEXT NOT NULL REFERENCES texts(id),
	user_id     TEXT NOT NULL REFERENCES users(id),
	cursor_pos  INTEGER NOT NULL,
	error_count INTEGER NOT NULL,
	cpm         INTEGER NOT NULL,
	started_at  DATETIME NOT NULL,
	updated_at  DATETIME NOT NULL,
	finished_at DATETIME
);
CREATE INDEX IF NOT EXISTS typing_sessions_text ON typing_sessions(text_id, updated_at);
CREATE INDEX IF NOT EXISTS typing_sessions_user ON typing_sessions(user_id, started_at);
-- login sessions (HTTP-only cookie); only the SHA-256 of the token is stored
CREATE TABLE IF NOT EXISTS auth_sessions (
	token_hash TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL REFERENCES users(id),
	created_at DATETIME NOT NULL,
	expires_at DATETIME NOT NULL
);
`

// Store wraps the database.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Open opens (and migrates) the SQLite database at path in WAL mode.
func Open(path string) (*Store, error) {
	// _txlock=immediate: a deferred transaction that reads first and writes later
	// cannot wait for the write lock in WAL mode and fails with SQLITE_BUSY at once
	// under concurrent syncs. IMMEDIATE takes the lock up front and honours busy_timeout.
	dsn := "file:" + path + "?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// NewID returns a random UUIDv4 string.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// slugAlphabet avoids ambiguous characters (0/o, 1/l/i) so codes can be read aloud.
const slugAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

func newSlug() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = slugAlphabet[int(b[i])%len(slugAlphabet)]
	}
	return string(b[:])
}

// ---- users & passkeys ----

type User struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

type Passkey struct {
	ID         string
	UserID     string
	PublicKey  []byte
	SignCount  uint32
	Credential []byte // JSON
}

// CreateUserWithPasskey inserts a user and its first passkey atomically.
func (s *Store) CreateUserWithPasskey(ctx context.Context, u User, pk Passkey) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO users(id, display_name, created_at) VALUES(?,?,?)`, u.ID, u.DisplayName, now); err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO passkeys(id, user_id, public_key, sign_count, credential, created_at) VALUES(?,?,?,?,?,?)`,
		pk.ID, u.ID, pk.PublicKey, pk.SignCount, pk.Credential, now); err != nil {
		return fmt.Errorf("insert passkey: %w", err)
	}
	return tx.Commit()
}

func (s *Store) GetUser(ctx context.Context, id string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `SELECT id, display_name, created_at FROM users WHERE id=?`, id).Scan(&u.ID, &u.DisplayName, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

func (s *Store) PasskeysForUser(ctx context.Context, userID string) ([]Passkey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, public_key, sign_count, credential FROM passkeys WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Passkey
	for rows.Next() {
		var p Passkey
		if err := rows.Scan(&p.ID, &p.UserID, &p.PublicKey, &p.SignCount, &p.Credential); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) UpdatePasskey(ctx context.Context, pk Passkey) error {
	res, err := s.db.ExecContext(ctx, `UPDATE passkeys SET sign_count=?, credential=? WHERE id=?`, pk.SignCount, pk.Credential, pk.ID)
	if err != nil {
		return err
	}
	return mustAffectOne(res)
}

// ---- auth sessions ----

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// CreateAuthSession returns a new random session token for userID.
func (s *Store) CreateAuthSession(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b[:])
	now := s.now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO auth_sessions(token_hash, user_id, created_at, expires_at) VALUES(?,?,?,?)`,
		hashToken(token), userID, now, now.Add(ttl))
	return token, err
}

// UserForToken resolves a session token; ErrNotFound if unknown or expired.
func (s *Store) UserForToken(ctx context.Context, token string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `SELECT u.id, u.display_name, u.created_at FROM auth_sessions a JOIN users u ON u.id=a.user_id
		WHERE a.token_hash=? AND a.expires_at > ?`, hashToken(token), s.now()).Scan(&u.ID, &u.DisplayName, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// DeleteExpiredAuthSessions removes expired logins and returns how many were deleted.
func (s *Store) DeleteExpiredAuthSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE expires_at <= ?`, s.now())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) DeleteAuthSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE token_hash=?`, hashToken(token))
	return err
}

// ---- texts ----

type Text struct {
	ID          string    `json:"id"`
	CreatorID   string    `json:"creator_id"`
	CreatorName string    `json:"creator_name"`
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateText stores a text (content must already be normalized) under a fresh slug.
func (s *Store) CreateText(ctx context.Context, creatorID, title, content string) (Text, error) {
	t := Text{CreatorID: creatorID, Title: title, Content: content, CreatedAt: s.now()}
	for range 10 {
		t.ID = newSlug()
		_, err := s.db.ExecContext(ctx, `INSERT INTO texts(id, creator_id, title, content, created_at) VALUES(?,?,?,?,?)`,
			t.ID, t.CreatorID, t.Title, t.Content, t.CreatedAt)
		if err == nil {
			return t, nil
		}
		var exists int
		if qerr := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM texts WHERE id=?`, t.ID).Scan(&exists); qerr != nil || exists == 0 {
			return t, fmt.Errorf("insert text: %w", err)
		}
		// slug collision: retry with a new slug
	}
	return t, errors.New("insert text: could not find a free slug")
}

func (s *Store) GetText(ctx context.Context, id string) (Text, error) {
	var t Text
	err := s.db.QueryRowContext(ctx, `SELECT t.id, t.creator_id, u.display_name, t.title, t.content, t.created_at
		FROM texts t JOIN users u ON u.id=t.creator_id WHERE t.id=?`, id).
		Scan(&t.ID, &t.CreatorID, &t.CreatorName, &t.Title, &t.Content, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

func (s *Store) TextsByCreator(ctx context.Context, creatorID string) ([]Text, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.creator_id, u.display_name, t.title, t.content, t.created_at
		FROM texts t JOIN users u ON u.id=t.creator_id WHERE t.creator_id=? ORDER BY t.created_at DESC`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Text{}
	for rows.Next() {
		var t Text
		if err := rows.Scan(&t.ID, &t.CreatorID, &t.CreatorName, &t.Title, &t.Content, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---- typing sessions ----

type TypingSession struct {
	ID         string     `json:"id"`
	TextID     string     `json:"text_id"`
	UserID     string     `json:"user_id"`
	UserName   string     `json:"user_name"`
	CursorPos  int        `json:"cursor_pos"`
	ErrorCount int        `json:"error_count"`
	CPM        int        `json:"cpm"`
	StartedAt  time.Time  `json:"started_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// UpsertTypingSession creates or updates a typing session owned by ts.UserID.
// Progress never goes backwards: cursor_pos and error_count keep their maximum,
// because sync requests may arrive out of order after a network outage.
// updated_at is always set to the server time.
// A new session is refused with ErrLimit if the user already has maxPerUser
// sessions on this text (0 = unlimited).
func (s *Store) UpsertTypingSession(ctx context.Context, ts TypingSession, maxPerUser int) (TypingSession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ts, err
	}
	defer tx.Rollback()
	// The text may have been deleted since the caller looked it up; check inside
	// the transaction so a concurrent delete yields ErrNotFound, not an FK error.
	var textExists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM texts WHERE id=?`, ts.TextID).Scan(&textExists); err != nil {
		return ts, err
	}
	if textExists == 0 {
		return ts, ErrNotFound
	}
	var owner, textID string
	err = tx.QueryRowContext(ctx, `SELECT user_id, text_id FROM typing_sessions WHERE id=?`, ts.ID).Scan(&owner, &textID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return ts, err
	case owner != ts.UserID || textID != ts.TextID:
		return ts, ErrForbidden
	}
	if errors.Is(err, sql.ErrNoRows) && maxPerUser > 0 {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM typing_sessions WHERE text_id=? AND user_id=?`, ts.TextID, ts.UserID).Scan(&n); err != nil {
			return ts, err
		}
		if n >= maxPerUser {
			return ts, ErrLimit
		}
	}
	ts.UpdatedAt = s.now()
	_, err = tx.ExecContext(ctx, `INSERT INTO typing_sessions(id, text_id, user_id, cursor_pos, error_count, cpm, started_at, updated_at, finished_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			cpm         = CASE WHEN excluded.cursor_pos >= cursor_pos THEN excluded.cpm ELSE cpm END,
			cursor_pos  = MAX(cursor_pos, excluded.cursor_pos),
			error_count = MAX(error_count, excluded.error_count),
			updated_at  = excluded.updated_at,
			finished_at = COALESCE(finished_at, excluded.finished_at)`,
		ts.ID, ts.TextID, ts.UserID, ts.CursorPos, ts.ErrorCount, ts.CPM, ts.StartedAt, ts.UpdatedAt, ts.FinishedAt)
	if err != nil {
		return ts, fmt.Errorf("upsert typing session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ts, err
	}
	return ts, nil
}

// TypingSessionsForText lists the most recently updated sessions of a text.
func (s *Store) TypingSessionsForText(ctx context.Context, textID string, limit int) ([]TypingSession, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.id, s.text_id, s.user_id, u.display_name, s.cursor_pos, s.error_count, s.cpm,
			s.started_at, s.updated_at, s.finished_at
		FROM typing_sessions s JOIN users u ON u.id=s.user_id
		WHERE s.text_id=? ORDER BY s.updated_at DESC LIMIT ?`, textID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TypingSession{}
	for rows.Next() {
		var ts TypingSession
		var fin sql.NullTime
		if err := rows.Scan(&ts.ID, &ts.TextID, &ts.UserID, &ts.UserName, &ts.CursorPos, &ts.ErrorCount, &ts.CPM,
			&ts.StartedAt, &ts.UpdatedAt, &fin); err != nil {
			return nil, err
		}
		if fin.Valid {
			ts.FinishedAt = &fin.Time
		}
		out = append(out, ts)
	}
	return out, rows.Err()
}

func mustAffectOne(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

// Result is a typing session of one user together with its text's title and length.
type Result struct {
	TypingSession
	TextTitle  string `json:"text_title"`
	TextLength int    `json:"text_length"` // in characters (runes)
}

// ResultsForUser lists a user's typing sessions, newest first. textID filters
// to one text if not empty.
func (s *Store) ResultsForUser(ctx context.Context, userID, textID string, limit int) ([]Result, error) {
	q := `SELECT s.id, s.text_id, s.user_id, u.display_name, s.cursor_pos, s.error_count, s.cpm,
			s.started_at, s.updated_at, s.finished_at, t.title, length(t.content)
		FROM typing_sessions s JOIN users u ON u.id=s.user_id JOIN texts t ON t.id=s.text_id
		WHERE s.user_id=?`
	args := []any{userID}
	if textID != "" {
		q += ` AND s.text_id=?`
		args = append(args, textID)
	}
	q += ` ORDER BY s.started_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Result{}
	for rows.Next() {
		var r Result
		var fin sql.NullTime
		if err := rows.Scan(&r.ID, &r.TextID, &r.UserID, &r.UserName, &r.CursorPos, &r.ErrorCount, &r.CPM,
			&r.StartedAt, &r.UpdatedAt, &fin, &r.TextTitle, &r.TextLength); err != nil {
			return nil, err
		}
		if fin.Valid {
			r.FinishedAt = &fin.Time
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteText deletes a text and all typing sessions on it. Only the creator may
// delete: ErrNotFound if the text does not exist, ErrForbidden if it is someone else's.
func (s *Store) DeleteText(ctx context.Context, textID, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var creator string
	err = tx.QueryRowContext(ctx, `SELECT creator_id FROM texts WHERE id=?`, textID).Scan(&creator)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if creator != userID {
		return ErrForbidden
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM typing_sessions WHERE text_id=?`, textID); err != nil {
		return fmt.Errorf("delete typing sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM texts WHERE id=?`, textID); err != nil {
		return fmt.Errorf("delete text: %w", err)
	}
	return tx.Commit()
}

// Best is the user's best finished session on one text.
type Best struct {
	ID     string `json:"id"`
	TextID string `json:"text_id"`
	CPM    int    `json:"cpm"`
}

// BestResults returns, per text, the user's finished session with the highest
// cpm (ties: the earlier one). Not limited, unlike ResultsForUser.
func (s *Store) BestResults(ctx context.Context, userID string) ([]Best, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, text_id, cpm FROM (
			SELECT id, text_id, cpm, ROW_NUMBER() OVER (PARTITION BY text_id ORDER BY cpm DESC, finished_at ASC) AS rn
			FROM typing_sessions WHERE user_id=? AND finished_at IS NOT NULL)
		WHERE rn=1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Best{}
	for rows.Next() {
		var b Best
		if err := rows.Scan(&b.ID, &b.TextID, &b.CPM); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SeedText is a built-in text (e.g. a lesson) owned by a system user.
type SeedText struct{ ID, Title, Content string }

// SeedTexts creates the system user and creates or updates the given texts
// under fixed ids. If a text got shorter, cursors of existing sessions are
// clamped so they stay valid. Idempotent; run at startup.
func (s *Store) SeedTexts(ctx context.Context, userID, userName string, texts []SeedText) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO users(id, display_name, created_at) VALUES(?,?,?)
		ON CONFLICT(id) DO UPDATE SET display_name=excluded.display_name`, userID, userName, now); err != nil {
		return fmt.Errorf("seed user: %w", err)
	}
	for _, t := range texts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO texts(id, creator_id, title, content, created_at) VALUES(?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET title=excluded.title, content=excluded.content
			WHERE creator_id=excluded.creator_id`, t.ID, userID, t.Title, t.Content, now); err != nil {
			return fmt.Errorf("seed text %s: %w", t.ID, err)
		}
		var creator string
		if err := tx.QueryRowContext(ctx, `SELECT creator_id FROM texts WHERE id=?`, t.ID).Scan(&creator); err != nil {
			return err
		}
		if creator != userID {
			return fmt.Errorf("seed text %s: id is taken by a text of user %s", t.ID, creator)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE typing_sessions SET cursor_pos=length(?) WHERE text_id=? AND cursor_pos > length(?)`,
			t.Content, t.ID, t.Content); err != nil {
			return fmt.Errorf("clamp sessions of %s: %w", t.ID, err)
		}
	}
	return tx.Commit()
}
