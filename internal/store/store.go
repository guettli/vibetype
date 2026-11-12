// Package store is the SQLite persistence layer of vibetype.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // CGO-free SQLite driver
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrDuplicate is returned when a row with the same unique key exists.
var ErrDuplicate = errors.New("already exists")

// ErrLastPasskey is returned when deleting a user's only passkey (it would lock the account).
var ErrLastPasskey = errors.New("cannot delete the last passkey")

// ErrForbidden is returned when a row belongs to another user.
var ErrForbidden = errors.New("forbidden")

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
	created_at DATETIME NOT NULL,
	label        TEXT NOT NULL DEFAULT '',     -- set by the user, e.g. "Laptop"
	last_used_at DATETIME
);
CREATE INDEX IF NOT EXISTS passkeys_user ON passkeys(user_id);
CREATE TABLE IF NOT EXISTS texts (
	id         TEXT PRIMARY KEY,
	creator_id TEXT NOT NULL REFERENCES users(id),
	title      TEXT NOT NULL,
	content    TEXT NOT NULL,
	created_at DATETIME NOT NULL,
	kind       TEXT NOT NULL DEFAULT '' -- '' = written by the user, 'practice' = generated for weak keys
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
	finished_at DATETIME,
	key_errors  TEXT NOT NULL DEFAULT '{}', -- JSON: expected character -> wrong key presses
	active_ms   INTEGER                     -- typing time without pauses; NULL = measured on wall time (older clients)
);
CREATE INDEX IF NOT EXISTS typing_sessions_text ON typing_sessions(text_id, updated_at);
CREATE INDEX IF NOT EXISTS typing_sessions_user ON typing_sessions(user_id, started_at);
CREATE INDEX IF NOT EXISTS typing_sessions_user_updated ON typing_sessions(user_id, updated_at);
-- local calendar days (YYYY-MM-DD, the typist's time zone) with at least one key
-- press, for the practice streak. Separate from typing_sessions so that pruning
-- or deleting sessions and texts does not shorten a streak after the fact.
CREATE TABLE IF NOT EXISTS practice_days (
	user_id TEXT NOT NULL REFERENCES users(id),
	day     TEXT NOT NULL,
	PRIMARY KEY (user_id, day)
) WITHOUT ROWID;
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
	var hadPracticeDays int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='practice_days'`).Scan(&hadPracticeDays); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	if hadPracticeDays == 0 {
		// Sessions from before practice_days: their UTC day of the last sync is the
		// best there is. updated_at is always written in UTC ("2006-01-02 ...").
		if _, err := db.Exec(`INSERT OR IGNORE INTO practice_days(user_id, day)
			SELECT user_id, substr(updated_at, 1, 10) FROM typing_sessions WHERE cursor_pos > 0`); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate %s: backfill practice_days: %w", path, err)
		}
	}
	for _, c := range []struct{ table, column, decl string }{
		{"typing_sessions", "key_errors", `TEXT NOT NULL DEFAULT '{}'`},
		{"texts", "kind", `TEXT NOT NULL DEFAULT ''`},
		{"passkeys", "label", `TEXT NOT NULL DEFAULT ''`},
		{"passkeys", "last_used_at", `DATETIME`},
		{"typing_sessions", "active_ms", `INTEGER`},
	} {
		if err := addColumnIfMissing(db, c.table, c.column, c.decl); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate %s: %w", path, err)
		}
	}
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}, nil
}

// addColumnIfMissing adds a column to a table created by an older version
// (CREATE TABLE IF NOT EXISTS does not change existing tables). The arguments
// are identifiers spliced into SQL: only ever pass constants.
func addColumnIfMissing(db *sql.DB, table, column, decl string) error {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, decl))
	return err
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
	// LoggedInAt is when the current login session was created (set by UserForToken only).
	LoggedInAt time.Time `json:"-"`
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
	res, err := s.db.ExecContext(ctx, `UPDATE passkeys SET sign_count=?, credential=?, last_used_at=? WHERE id=?`, pk.SignCount, pk.Credential, s.now(), pk.ID)
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
	err := s.db.QueryRowContext(ctx, `SELECT u.id, u.display_name, u.created_at, a.created_at FROM auth_sessions a JOIN users u ON u.id=a.user_id
		WHERE a.token_hash=? AND a.expires_at > ?`, hashToken(token), s.now()).Scan(&u.ID, &u.DisplayName, &u.CreatedAt, &u.LoggedInAt)
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
	return s.createText(ctx, s.db, creatorID, title, content, "")
}

// execQuerier is what createText needs from *sql.DB or *sql.Tx.
type execQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Store) createText(ctx context.Context, db execQuerier, creatorID, title, content, kind string) (Text, error) {
	t := Text{CreatorID: creatorID, Title: title, Content: content, CreatedAt: s.now()}
	for range 10 {
		t.ID = newSlug()
		_, err := db.ExecContext(ctx, `INSERT INTO texts(id, creator_id, title, content, created_at, kind) VALUES(?,?,?,?,?,?)`,
			t.ID, t.CreatorID, t.Title, t.Content, t.CreatedAt, kind)
		if err == nil {
			return t, nil
		}
		var exists int
		if qerr := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM texts WHERE id=?`, t.ID).Scan(&exists); qerr != nil || exists == 0 {
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
	// KeyErrors maps an expected character to the number of wrong key presses
	// while it was due. Only written by UpsertTypingSession, not read by the lists.
	KeyErrors map[string]int `json:"key_errors,omitempty"`
	// ActiveMS is the typing time without pauses; nil for runs of older clients,
	// whose cpm includes pauses and is therefore not comparable.
	ActiveMS *int64 `json:"active_ms"`
	// PracticeDays are the typist's local days (YYYY-MM-DD) with key presses in
	// this session; UpsertTypingSession adds them to practice_days.
	PracticeDays []string `json:"-"`
}

// UpsertTypingSession creates or updates a typing session owned by ts.UserID.
// Progress never goes backwards: cursor_pos and error_count keep their maximum,
// because sync requests may arrive out of order after a network outage.
// updated_at is always set to the server time.
// A user keeps at most maxPerUser sessions per text (0 = unlimited): when a new
// session would exceed that, the oldest sessions are deleted, except the user's
// best finished one. So repeating a lesson forever works and storage stays bounded.
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
			if _, err := tx.ExecContext(ctx, `DELETE FROM typing_sessions WHERE id IN (
					SELECT id FROM typing_sessions WHERE text_id=? AND user_id=? AND id NOT IN (
						SELECT id FROM typing_sessions WHERE text_id=? AND user_id=? AND finished_at IS NOT NULL
						ORDER BY (active_ms IS NOT NULL) DESC, cpm DESC, finished_at ASC LIMIT 1)
					ORDER BY started_at ASC LIMIT ?)`,
				ts.TextID, ts.UserID, ts.TextID, ts.UserID, n-maxPerUser+1); err != nil {
				return ts, fmt.Errorf("prune old sessions: %w", err)
			}
		}
	}
	ts.UpdatedAt = s.now()
	keyErrors, err := json.Marshal(ts.KeyErrors)
	if err != nil {
		return ts, err
	}
	if ts.KeyErrors == nil {
		keyErrors = []byte("{}")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO typing_sessions(id, text_id, user_id, cursor_pos, error_count, cpm, started_at, updated_at, finished_at, key_errors, active_ms)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			cpm         = CASE WHEN excluded.cursor_pos >= cursor_pos THEN excluded.cpm ELSE cpm END,
			active_ms   = CASE WHEN excluded.cursor_pos >= cursor_pos THEN excluded.active_ms ELSE active_ms END,
			key_errors  = CASE WHEN excluded.error_count >= error_count THEN excluded.key_errors ELSE key_errors END,
			cursor_pos  = MAX(cursor_pos, excluded.cursor_pos),
			error_count = MAX(error_count, excluded.error_count),
			updated_at  = excluded.updated_at,
			finished_at = COALESCE(finished_at, excluded.finished_at)`,
		ts.ID, ts.TextID, ts.UserID, ts.CursorPos, ts.ErrorCount, ts.CPM, ts.StartedAt, ts.UpdatedAt, ts.FinishedAt, string(keyErrors), ts.ActiveMS)
	if err != nil {
		return ts, fmt.Errorf("upsert typing session: %w", err)
	}
	for _, day := range ts.PracticeDays {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO practice_days(user_id, day) VALUES(?,?)`, ts.UserID, day); err != nil {
			return ts, fmt.Errorf("record practice day: %w", err)
		}
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
			s.started_at, s.updated_at, s.finished_at, t.title, length(t.content), s.active_ms
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
			&r.StartedAt, &r.UpdatedAt, &fin, &r.TextTitle, &r.TextLength, &r.ActiveMS); err != nil {
			return nil, err
		}
		if fin.Valid {
			r.FinishedAt = &fin.Time
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Activity is one typing session reduced to what the progress chart needs.
type Activity struct {
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at"`
	CursorPos  int        `json:"cursor_pos"`
	ErrorCount int        `json:"error_count"`
	CPM        int        `json:"cpm"`
	ActiveMS   *int64     `json:"active_ms"`
}

// Times are stored as text by the driver and compared as strings, so every
// time parameter of a query must be in UTC like the stored values.

// ActivitySince lists a user's sessions with at least one typed character and
// a sync at or after since, newest first, at most limit.
func (s *Store) ActivitySince(ctx context.Context, userID string, since time.Time, limit int) ([]Activity, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT updated_at, finished_at, cursor_pos, error_count, cpm, active_ms
		FROM typing_sessions WHERE user_id=? AND cursor_pos > 0 AND updated_at >= ?
		ORDER BY updated_at DESC LIMIT ?`, userID, since.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Activity{}
	for rows.Next() {
		var a Activity
		var fin sql.NullTime
		if err := rows.Scan(&a.UpdatedAt, &fin, &a.CursorPos, &a.ErrorCount, &a.CPM, &a.ActiveMS); err != nil {
			return nil, err
		}
		if fin.Valid {
			a.FinishedAt = &fin.Time
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// PracticeDaysSince returns a user's practice days (YYYY-MM-DD) from sinceDay on, oldest first.
func (s *Store) PracticeDaysSince(ctx context.Context, userID, sinceDay string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT day FROM practice_days WHERE user_id=? AND day >= ? ORDER BY day`, userID, sinceDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
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
			SELECT id, text_id, cpm, ROW_NUMBER() OVER (PARTITION BY text_id
				-- runs measured without pauses (active_ms) beat older wall-time runs: not comparable
				ORDER BY (active_ms IS NOT NULL) DESC, cpm DESC, finished_at ASC) AS rn
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
// under fixed ids. If a text's content changed, its sessions are deleted (their
// progress refers to the old text; clients reset their local state when they
// see the new content). Texts of the system user that are not in texts any
// more are deleted with their sessions. Idempotent; run at startup. It returns
// the number of deleted sessions so the caller can log it.
func (s *Store) SeedTexts(ctx context.Context, userID, userName string, texts []SeedText) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := s.now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO users(id, display_name, created_at) VALUES(?,?,?)
		ON CONFLICT(id) DO UPDATE SET display_name=excluded.display_name`, userID, userName, now); err != nil {
		return 0, fmt.Errorf("seed user: %w", err)
	}
	var deleted int64
	keep := map[string]bool{}
	for _, t := range texts {
		keep[t.ID] = true
		var creator, oldContent string
		err := tx.QueryRowContext(ctx, `SELECT creator_id, content FROM texts WHERE id=?`, t.ID).Scan(&creator, &oldContent)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := tx.ExecContext(ctx, `INSERT INTO texts(id, creator_id, title, content, created_at) VALUES(?,?,?,?,?)`,
				t.ID, userID, t.Title, t.Content, now); err != nil {
				return 0, fmt.Errorf("seed text %s: %w", t.ID, err)
			}
			continue
		case err != nil:
			return 0, err
		case creator != userID:
			return 0, fmt.Errorf("seed text %s: id is taken by a text of user %s", t.ID, creator)
		}
		if oldContent != t.Content {
			res, err := tx.ExecContext(ctx, `DELETE FROM typing_sessions WHERE text_id=?`, t.ID)
			if err != nil {
				return 0, fmt.Errorf("reset sessions of %s: %w", t.ID, err)
			}
			n, _ := res.RowsAffected()
			deleted += n
		}
		if _, err := tx.ExecContext(ctx, `UPDATE texts SET title=?, content=? WHERE id=?`, t.Title, t.Content, t.ID); err != nil {
			return 0, fmt.Errorf("update text %s: %w", t.ID, err)
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM texts WHERE creator_id=?`, userID)
	if err != nil {
		return 0, err
	}
	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		if !keep[id] {
			stale = append(stale, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range stale {
		res, err := tx.ExecContext(ctx, `DELETE FROM typing_sessions WHERE text_id=?`, id)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		deleted += n
		if _, err := tx.ExecContext(ctx, `DELETE FROM texts WHERE id=?`, id); err != nil {
			return 0, err
		}
	}
	return deleted, tx.Commit()
}

// KeyStat is a user's error statistics for one character.
type KeyStat struct {
	Char     string  `json:"char"`
	Errors   int     `json:"errors"`   // wrong key presses while this character was due
	Attempts int     `json:"attempts"` // correct presses + errors
	Rate     float64 `json:"rate"`     // Errors / Attempts
}

// WeakKeys aggregates the key errors of the user's newest sessions (at most
// `sessions`) started after since. A character is only rated after minAttempts
// attempts; the result is sorted by error rate, worst first, at most `limit`.
func (s *Store) WeakKeys(ctx context.Context, userID string, since time.Time, sessions, minAttempts, limit int) ([]KeyStat, error) {
	// Sessions with errors but empty key_errors (written before key tracking
	// existed, or resumed from an old client state) would add correct presses
	// without the matching errors and dilute the rates, so they are skipped.
	rows, err := s.db.QueryContext(ctx, `SELECT t.content, s.cursor_pos, s.key_errors
		FROM typing_sessions s JOIN texts t ON t.id=s.text_id
		WHERE s.user_id=? AND s.started_at >= ? AND (s.key_errors != '{}' OR s.error_count = 0)
		ORDER BY s.started_at DESC LIMIT ?`, userID, since.UTC(), sessions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type typed struct {
		content string
		cursor  int
	}
	var sessionsTyped []typed
	errs := map[rune]int{}
	for rows.Next() {
		var content, keyErrors string
		var cursor int
		if err := rows.Scan(&content, &cursor, &keyErrors); err != nil {
			return nil, err
		}
		var ke map[string]int
		if err := json.Unmarshal([]byte(keyErrors), &ke); err != nil {
			return nil, fmt.Errorf("decode key_errors: %w", err)
		}
		for k, n := range ke {
			if r := []rune(k); len(r) == 1 {
				errs[r[0]] += n
			}
		}
		sessionsTyped = append(sessionsTyped, typed{content, cursor})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// count correct presses only for characters that have errors at all
	correct := map[rune]int{}
	for _, st := range sessionsTyped {
		i := 0
		for _, r := range st.content {
			if i >= st.cursor {
				break
			}
			if _, ok := errs[r]; ok {
				correct[r]++
			}
			i++
		}
	}
	out := []KeyStat{}
	for r, e := range errs {
		k := string(r)
		attempts := correct[r] + e
		if e == 0 || attempts < minAttempts {
			continue
		}
		out = append(out, KeyStat{Char: k, Errors: e, Attempts: attempts, Rate: float64(e) / float64(attempts)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rate != out[j].Rate {
			return out[i].Rate > out[j].Rate
		}
		return out[i].Char < out[j].Char
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// CreatePracticeText stores a generated practice text and deletes the user's
// older practice texts (with their sessions) beyond the newest `keep`, so
// repeated clicks do not pile up texts.
func (s *Store) CreatePracticeText(ctx context.Context, userID, title, content string, keep int) (Text, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Text{}, err
	}
	defer tx.Rollback()
	t, err := s.createText(ctx, tx, userID, title, content, "practice")
	if err != nil {
		return t, err
	}
	old := `SELECT id FROM texts WHERE creator_id=? AND kind='practice' ORDER BY created_at DESC, rowid DESC LIMIT -1 OFFSET ?`
	if _, err := tx.ExecContext(ctx, `DELETE FROM typing_sessions WHERE text_id IN (`+old+`)`, userID, keep); err != nil {
		return t, fmt.Errorf("prune practice sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM texts WHERE id IN (`+old+`)`, userID, keep); err != nil {
		return t, fmt.Errorf("prune practice texts: %w", err)
	}
	return t, tx.Commit()
}

// PasskeyInfo is what a user sees about one of their passkeys.
type PasskeyInfo struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// AddPasskey stores an additional passkey for an existing user.
func (s *Store) AddPasskey(ctx context.Context, pk Passkey, label string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO passkeys(id, user_id, public_key, sign_count, credential, created_at, label) VALUES(?,?,?,?,?,?,?)`,
		pk.ID, pk.UserID, pk.PublicKey, pk.SignCount, pk.Credential, s.now(), label)
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrDuplicate
	}
	return err
}

// DeleteOtherAuthSessions logs the user out everywhere except the session of keepToken.
func (s *Store) DeleteOtherAuthSessions(ctx context.Context, userID, keepToken string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE user_id=? AND token_hash != ?`, userID, hashToken(keepToken))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListPasskeys lists a user's passkeys, oldest first.
func (s *Store) ListPasskeys(ctx context.Context, userID string) ([]PasskeyInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, label, created_at, last_used_at FROM passkeys WHERE user_id=? ORDER BY created_at, rowid`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PasskeyInfo{}
	for rows.Next() {
		var p PasskeyInfo
		var last sql.NullTime
		if err := rows.Scan(&p.ID, &p.Label, &p.CreatedAt, &last); err != nil {
			return nil, err
		}
		if last.Valid {
			p.LastUsedAt = &last.Time
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeletePasskey deletes one of the user's passkeys. ErrNotFound if the user has
// no such passkey, ErrLastPasskey if it is the only one.
func (s *Store) DeletePasskey(ctx context.Context, userID, passkeyID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM passkeys WHERE user_id=?`, userID).Scan(&n); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM passkeys WHERE id=? AND user_id=?`, passkeyID, userID)
	if err != nil {
		return err
	}
	if err := mustAffectOne(res); err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastPasskey // rolled back
	}
	return tx.Commit()
}

// RenameUser changes a user's display name.
func (s *Store) RenameUser(ctx context.Context, userID, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET display_name=? WHERE id=?`, name, userID)
	if err != nil {
		return err
	}
	return mustAffectOne(res)
}
