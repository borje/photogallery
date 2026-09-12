// Command smugbox runs the photo gallery server and its admin CLI.
//
//	smugbox serve
//	smugbox admin create-api-key --label "Lightroom laptop"
//	smugbox admin revoke-api-key <id>
//	smugbox admin list-api-keys
//	smugbox admin list-albums
//	smugbox admin delete-album <slug>
//	smugbox admin set-password <slug> [--clear]
//	smugbox admin gc [--dry-run]
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bege/smugbox/backend/internal/api"
	"github.com/bege/smugbox/backend/internal/config"
	"github.com/bege/smugbox/backend/internal/db"
	"github.com/bege/smugbox/backend/internal/image"
	"github.com/bege/smugbox/backend/internal/storage"
	"github.com/bege/smugbox/backend/internal/web"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() error {
	return errors.New("usage: smugbox serve | smugbox admin <create-api-key|revoke-api-key|list-api-keys|list-albums|delete-album|set-password|gc> [flags]")
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		return err
	}
	switch args[0] {
	case "serve":
		return serve(cfg)
	case "admin":
		return admin(cfg, args[1:])
	default:
		return usage()
	}
}

func newLogger(cfg config.Config) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

// openData prepares the data directory, database and file store.
func openData(ctx context.Context, cfg config.Config) (*db.DB, *storage.Store, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create data dir: %w", err)
	}
	database, err := db.Open(ctx, filepath.Join(cfg.DataDir, "smugbox.db"))
	if err != nil {
		return nil, nil, err
	}
	store, err := storage.New(cfg.DataDir)
	if err != nil {
		database.Close()
		return nil, nil, err
	}
	return database, store, nil
}

func serve(cfg config.Config) error {
	logger := newLogger(cfg)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	image.Startup()
	defer image.Shutdown()

	database, store, err := openData(ctx, cfg)
	if err != nil {
		return err
	}
	defer database.Close()

	handler, err := api.New(api.Deps{
		DB:    database,
		Store: store,
		Cfg:   cfg,
		Log:   logger,
		Web:   web.Handler(cfg.FrontendDir),
	})
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: zip downloads may legitimately take minutes.
	}

	// Display variants are rendered here, after each upload has been
	// acknowledged; pending photos from before a restart are picked up first.
	workerDone := make(chan struct{})
	go func() {
		handler.Run(ctx)
		close(workerDone)
	}()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.ListenAddr, "data_dir", cfg.DataDir, "frontend_dir", cfg.FrontendDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	stop() // also stops the variant worker after the photo it is on
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	<-workerDone
	return nil
}
