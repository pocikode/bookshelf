package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"pocikode/bookshelf/server/internal/auth"
	"pocikode/bookshelf/server/internal/config"
	"pocikode/bookshelf/server/internal/database"
	"pocikode/bookshelf/server/internal/httpapi"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		response, err := http.Get("http://127.0.0.1:" + getenv("PORT", "3000") + "/api/health")
		if err != nil || response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		return
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "books"), 0o750); err != nil {
		log.Fatal(err)
	}
	db, err := database.Open(cfg.DatabasePath())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := bootstrapUser(db.DB); err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: ":" + cfg.Port, Handler: httpapi.New(db.DB, cfg).Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		log.Printf("readest personal listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func bootstrapUser(db *sql.DB) error {
	username, password := os.Getenv("ADMIN_USERNAME"), os.Getenv("ADMIN_PASSWORD")
	if username == "" || password == "" {
		return nil
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	_, err = db.Exec(`INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, auth.NewID(), username, hash, now, now)
	return err
}
