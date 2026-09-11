package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pocikode/bookshelf/server/internal/auth"
	"pocikode/bookshelf/server/internal/config"
	"pocikode/bookshelf/server/internal/database"
	"pocikode/bookshelf/server/internal/httpapi"
	"pocikode/bookshelf/server/internal/logging"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	os.Exit(run())
}

// run returns an exit code instead of calling log.Fatal so deferred cleanup —
// notably closing the database — still happens on every failure path.
func run() int {
	// Bootstrap logger: configuration itself can fail, and that failure has to
	// be reported before LOG_LEVEL/LOG_FORMAT are known.
	logger := logging.New(os.Getenv("LOG_LEVEL"), os.Getenv("LOG_FORMAT"))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("could not load configuration", slog.String("error", err.Error()))
		return 1
	}
	logger = logging.New(cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(logger)

	for _, dir := range []string{cfg.BooksDir(), cfg.CoversDir(), cfg.UploadsDir(), cfg.TrashDir()} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			logger.Error("could not create data directory",
				slog.String("path", dir), slog.String("error", err.Error()))
			return 1
		}
	}
	db, err := database.Open(cfg.DatabasePath())
	if err != nil {
		logger.Error("could not open database",
			slog.String("path", cfg.DatabasePath()), slog.String("error", err.Error()))
		return 1
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("could not close database", slog.String("error", err.Error()))
		}
	}()
	if err := bootstrapUser(db.DB); err != nil {
		logger.Error("could not bootstrap admin user", slog.String("error", err.Error()))
		return 1
	}

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: httpapi.New(db.DB, cfg, logger).Handler(),
		// Route net/http's own errors (TLS handshakes, malformed requests)
		// into the structured logger instead of the unstructured default.
		ErrorLog:          slog.NewLogLogger(logger.With(slog.String("source", "net/http")).Handler(), slog.LevelError),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("server listening",
			slog.String("addr", server.Addr),
			slog.String("dataDir", cfg.DataDir),
			slog.String("staticDir", cfg.StaticDir))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			logger.Error("server stopped", slog.String("addr", server.Addr), slog.String("error", err.Error()))
			return 1
		}
		return 0
	case signal := <-stop:
		logger.Info("shutdown requested", slog.String("signal", signal.String()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		// In-flight requests were cut off; that is worth knowing.
		logger.Error("graceful shutdown failed", slog.String("error", err.Error()))
		return 1
	}
	logger.Info("shutdown complete")
	return 0
}

// healthcheck backs the container HEALTHCHECK. It logs the reason it failed so
// a restart loop is diagnosable from the container logs alone.
func healthcheck() int {
	url := "http://127.0.0.1:" + getenv("PORT", "3000") + "/api/health"
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		log.Printf("healthcheck: request to %s failed: %v", url, err)
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		log.Printf("healthcheck: %s returned %d", url, response.StatusCode)
		return 1
	}
	return 0
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func bootstrapUser(db *sql.DB) error {
	username, password := os.Getenv("ADMIN_USERNAME"), os.Getenv("ADMIN_PASSWORD")
	if (username == "") != (password == "") {
		return fmt.Errorf("ADMIN_USERNAME and ADMIN_PASSWORD must be set together")
	}
	if username == "" {
		return nil
	}
	var admins int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&admins); err != nil {
		return fmt.Errorf("count admin users: %w", err)
	}
	if admins > 0 {
		return nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash bootstrap password: %w", err)
	}
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash, role, created_at, updated_at) VALUES (?, ?, ?, 'admin', ?, ?)`, auth.NewID(), username, hash, now, now); err != nil {
		return fmt.Errorf("insert bootstrap user %q: %w", username, err)
	}
	slog.Info("bootstrap admin user created", slog.String("username", username))
	return nil
}
