package store

import (
	"context"
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
	for i := range 3 {
		if _, err := st.UpsertTypingSession(ctx, TypingSession{ID: fmt.Sprint("s", i), TextID: txt.ID, UserID: u.ID, StartedAt: time.Now()}, 2); i < 2 && err != nil || i == 2 && !errors.Is(err, ErrLimit) {
			t.Fatalf("session %d: %v", i, err)
		}
	}
	// updating an existing session is always allowed
	if _, err := st.UpsertTypingSession(ctx, TypingSession{ID: "s0", TextID: txt.ID, UserID: u.ID, CursorPos: 1, StartedAt: time.Now()}, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAuthSession(ctx, u.ID, -time.Second); err != nil {
		t.Fatal(err)
	}
	if n, err := st.DeleteExpiredAuthSessions(ctx); err != nil || n != 1 {
		t.Fatalf("expired sessions: %d %v", n, err)
	}
}
