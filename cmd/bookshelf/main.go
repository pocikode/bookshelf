package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"pocikode/bookshelf/internal/auth"
	"pocikode/bookshelf/internal/bookmark"
	"pocikode/bookshelf/internal/config"
	"pocikode/bookshelf/internal/database"
	"pocikode/bookshelf/internal/library"
	"pocikode/bookshelf/internal/progress"
	"pocikode/bookshelf/internal/ratelimit"
	"pocikode/bookshelf/internal/version"
	"pocikode/bookshelf/internal/web"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "bookshelf:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}
	switch command {
	case "serve":
		if len(args) > 1 {
			return errors.New("serve accepts no arguments")
		}
		return serve()
	case "healthcheck":
		if len(args) > 1 {
			return errors.New("healthcheck accepts no arguments")
		}
		return healthcheck()
	case "version":
		if len(args) > 1 {
			return errors.New("version accepts no arguments")
		}
		fmt.Println(version.Version)
		return nil
	default:
		return fmt.Errorf("unknown command %q (use serve, healthcheck, or version)", command)
	}
}

func serve() error {
	if err := config.LoadDotEnv(".env"); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err = cfg.PrepareDataDir(); err != nil {
		return err
	}
	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	startupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := database.Open(startupCtx, cfg.DataDir)
	if err != nil {
		return err
	}
	repo := database.NewRepository(db)
	libraryService := library.New(repo, cfg.DataDir, cfg.MaxUploadBytes)
	if err = libraryService.CleanupStaleUploads(24 * time.Hour); err != nil {
		db.Close()
		return fmt.Errorf("reconcile temporary uploads: %w", err)
	}
	if err = libraryService.ReconcileTrash(startupCtx); err != nil {
		db.Close()
		return fmt.Errorf("reconcile deletion trash: %w", err)
	}
	authService := auth.New(repo, cfg.Password, cfg.SessionDays)
	if err = authService.Initialize(startupCtx, cfg.Password); err != nil {
		db.Close()
		return fmt.Errorf("initialize password credential: %w", err)
	}
	if cfg.UsingDefaultPassword() && authService.ComparePassword(config.DefaultPassword) {
		logger.Warn("default_password_in_use",
			"event", "default_password_in_use",
			"advice", "APP_PASSWORD was not set; the built-in default password is in use, set APP_PASSWORD before exposing this service")
	}
	handler, err := web.NewServer(web.Dependencies{Config: cfg, Repository: repo, Auth: authService, Limiter: ratelimit.New(nil), Library: libraryService, Progress: progress.New(repo), Bookmark: bookmark.New(repo), Logger: logger})
	if err != nil {
		db.Close()
		return err
	}
	server := &http.Server{Addr: ":" + strconv.Itoa(cfg.Port), Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	workerCtx, stopWorkers := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	workers.Add(1)
	go func() { defer workers.Done(); sessionSweeper(workerCtx, repo, logger) }()
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("server_started", "event", "server_started", "port", cfg.Port)
		serveErr <- server.ListenAndServe()
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	var cause error
	select {
	case sig := <-signals:
		logger.Info("shutdown_requested", "event", "shutdown_requested", "signal", sig.String())
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			cause = err
		}
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil && cause == nil {
		cause = fmt.Errorf("http shutdown: %w", err)
	}
	stopWorkers()
	workers.Wait()
	checkpointCtx, checkpointCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer checkpointCancel()
	if err := database.Checkpoint(checkpointCtx, db); err != nil && cause == nil {
		cause = fmt.Errorf("checkpoint database: %w", err)
	}
	if err := db.Close(); err != nil && cause == nil {
		cause = err
	}
	logger.Info("server_stopped", "event", "server_stopped")
	return cause
}

func sessionSweeper(ctx context.Context, repo *database.Repository, logger *slog.Logger) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			n, err := repo.SweepSessions(ctx, now.UTC())
			if err != nil {
				logger.Error("session_sweep_failed", "event", "session_sweep_failed", "error", err)
			} else {
				logger.Info("session_sweep_complete", "event", "session_sweep_complete", "deleted", n)
			}
		case <-ctx.Done():
			return
		}
	}
}
func healthcheck() error {
	port := config.DefaultPort
	if raw := os.Getenv("PORT"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 65535 {
			return errors.New("PORT must be an integer between 1 and 65535")
		}
		port = parsed
	}
	client := http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/healthz")
	if err != nil {
		return fmt.Errorf("health request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health status: %s", response.Status)
	}
	return nil
}
func newLogger(level string) *slog.Logger {
	var configured slog.Level
	switch level {
	case "debug":
		configured = slog.LevelDebug
	case "warn":
		configured = slog.LevelWarn
	case "error":
		configured = slog.LevelError
	default:
		configured = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: configured}))
}
