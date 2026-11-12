package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Many typists syncing at the same time must not hit SQLITE_BUSY.
func TestConcurrentUpserts(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	u := User{ID: NewID(), DisplayName: "u"}
	if err := st.CreateUserWithPasskey(ctx, u, Passkey{ID: "pk", PublicKey: []byte{1}, Credential: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	txt, err := st.CreateText(ctx, u.ID, "t", "hello world")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errc := make(chan error, 40*30)
	for g := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("session-%02d", g)
			for i := range 30 {
				_, err := st.UpsertTypingSession(ctx, TypingSession{ID: id, TextID: txt.ID, UserID: u.ID,
					CursorPos: i % 11, StartedAt: time.Now()}, 0)
				if err != nil {
					errc <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Fatal(err)
	}
}

func TestSessionLimitAndExpiry(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	u := User{ID: NewID(), DisplayName: "u"}
	if err := st.CreateUserWithPasskey(ctx, u, Passkey{ID: "pk", PublicKey: []byte{1}, Credential: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	txt, _ := st.CreateText(ctx, u.ID, "t", "abc")
	// s0 is the best finished run; s1..s4 are newer, unfinished ones. With a
	// limit of 3, the oldest runs are pruned but the best one survives.
	t0 := time.Now().Add(-time.Hour)
	fin := t0.Add(time.Minute)
	if _, err := st.UpsertTypingSession(ctx, TypingSession{ID: "s0", TextID: txt.ID, UserID: u.ID, CursorPos: 3, CPM: 500, StartedAt: t0, FinishedAt: &fin}, 3); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 4; i++ {
		if _, err := st.UpsertTypingSession(ctx, TypingSession{ID: fmt.Sprint("s", i), TextID: txt.ID, UserID: u.ID, StartedAt: t0.Add(time.Duration(i) * time.Minute)}, 3); err != nil {
			t.Fatalf("session %d: %v", i, err)
		}
	}
	res, err := st.ResultsForUser(ctx, u.ID, txt.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range res {
		ids = append(ids, r.ID)
	}
	if fmt.Sprint(ids) != "[s4 s3 s0]" {
		t.Fatalf("after pruning: %v", ids)
	}
	// updating an existing session never prunes
	if _, err := st.UpsertTypingSession(ctx, TypingSession{ID: "s3", TextID: txt.ID, UserID: u.ID, CursorPos: 1, StartedAt: time.Now()}, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAuthSession(ctx, u.ID, -time.Second); err != nil {
		t.Fatal(err)
	}
	if n, err := st.DeleteExpiredAuthSessions(ctx); err != nil || n != 1 {
		t.Fatalf("expired sessions: %d %v", n, err)
	}
}

func TestUpsertOnDeletedText(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	u := User{ID: NewID(), DisplayName: "u"}
	if err := st.CreateUserWithPasskey(ctx, u, Passkey{ID: "pk", PublicKey: []byte{1}, Credential: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	// the handler looked the text up, then it was deleted before the upsert
	if _, err := st.UpsertTypingSession(ctx, TypingSession{ID: "s", TextID: "gone00", UserID: u.ID, StartedAt: time.Now()}, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// A database created by an older version (no key_errors column) is migrated on Open.
func TestMigrateAddsKeyErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE typing_sessions (id TEXT PRIMARY KEY, text_id TEXT NOT NULL, user_id TEXT NOT NULL,
		cursor_pos INTEGER NOT NULL, error_count INTEGER NOT NULL, cpm INTEGER NOT NULL, started_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL, finished_at DATETIME);
		INSERT INTO typing_sessions VALUES('old','t','u',1,0,10,'2026-01-01','2026-01-01',NULL)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var ke string
	if err := st.db.QueryRow(`SELECT key_errors FROM typing_sessions WHERE id='old'`).Scan(&ke); err != nil || ke != "{}" {
		t.Fatalf("migrated row: %q %v", ke, err)
	}
	// opening again is a no-op
	st.Close()
	if st, err = Open(path); err != nil {
		t.Fatal(err)
	}
}
