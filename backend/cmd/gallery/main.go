// Command gallery runs the photo gallery server and its admin CLI.
//
//	gallery serve
//	gallery admin create-api-key --label "Lightroom laptop"
//	gallery admin revoke-api-key <id>
//	gallery admin list-api-keys
//	gallery admin list-albums
//	gallery admin delete-album <slug>
//	gallery admin set-password <slug> [--clear]
//	gallery admin gc [--dry-run]
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

	"github.com/bege/photogallery/backend/internal/api"
	"github.com/bege/photogallery/backend/internal/config"
	"github.com/bege/photogallery/backend/internal/db"
	"github.com/bege/photogallery/backend/internal/image"
	"github.com/bege/photogallery/backend/internal/storage"
	"github.com/bege/photogallery/backend/internal/web"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() error {
	return errors.New("usage: gallery serve | gallery admin <create-api-key|revoke-api-key|list-api-keys|list-albums|delete-album|set-password|gc> [flags]")
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
	database, err := db.Open(ctx, filepath.Join(cfg.DataDir, "gallery.db"))
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
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
