// Package logging provides the process-wide structured logger, a
// request-scoped logger carried on the request context, and the HTTP
// middleware that turns every request — and every panic — into a log event.
//
// The rule the rest of the backend follows: an error value is never discarded.
// Either it is returned to a caller that logs it, or it is logged here.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

type contextKey string

const (
	loggerKey    contextKey = "logger"
	requestIDKey contextKey = "requestID"
)

// New builds the root logger. Format is "json" (default) or "text"; level is
// one of debug, info, warn, error and falls back to info when unrecognised.
func New(level, format string) *slog.Logger {
	options := &slog.HandlerOptions{Level: ParseLevel(level)}
	var handler slog.Handler
	if strings.EqualFold(format, "text") {
		handler = slog.NewTextHandler(os.Stderr, options)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, options)
	}
	return slog.New(handler)
}

// ParseLevel maps a LOG_LEVEL string onto a slog level, defaulting to info.
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// WithLogger returns a context carrying logger.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// From returns the request-scoped logger, or the default logger when the
// context predates the middleware (tests, background work).
func From(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey).(*slog.Logger); ok && logger != nil {
		return logger
	}
	return slog.Default()
}

// RequestID returns the id assigned by the middleware, or "" outside a request.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}
