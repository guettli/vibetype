// vibetype: a minimal, offline-capable touch typing trainer.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/guettli/vibetype/internal/server"
	"github.com/guettli/vibetype/internal/store"
	"github.com/guettli/vibetype/web"
)

// version is set at build time: -ldflags "-X main.version=v0.0.1"
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "vibetype:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", envOr("VIBETYPE_ADDR", ":8080"), "listen address")
	dbPath := flag.String("db", envOr("VIBETYPE_DB", "vibetype.db"), "SQLite database file")
	origin := flag.String("origin", envOr("VIBETYPE_ORIGIN", "http://localhost:8080"),
		"public origin, e.g. https://vibetype.thomas-guettler.de (WebAuthn rpId is derived from its host)")
	flag.Parse()

	u, err := url.Parse(*origin)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" {
		return fmt.Errorf("invalid -origin %q: want scheme://host[:port]", *origin)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := server.SeedLessons(context.Background(), st); err != nil {
		return fmt.Errorf("seed lessons: %w", err)
	}

	srv, err := server.New(server.Config{
		Version: version, RPID: u.Hostname(), Origin: *origin, Static: web.Static(), Logger: logger, Store: st,
	})
	if err != nil {
		return err
	}

	hs := &http.Server{
		Addr:              *addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go srv.RunJanitor(ctx, time.Hour)
	errc := make(chan error, 1)
	go func() {
		logger.Info("listening", "version", version, "addr", *addr, "origin", *origin, "db", *dbPath)
		errc <- hs.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := hs.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
