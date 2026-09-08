package logging

import (
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
)

// RequestIDHeader is echoed back on every response so a client-reported
// failure can be located in the logs.
const RequestIDHeader = "X-Request-ID"

// responseRecorder captures what was actually sent so the access log can
// report status and size without the handlers cooperating.
type responseRecorder struct {
	http.ResponseWriter
	status  int
	written int64
	wrote   bool
}

func (w *responseRecorder) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.status = status
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseRecorder) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.written += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer, keeping
// flushing and hijacking (http.ServeContent, range requests) working.
func (w *responseRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Middleware assigns a request id, exposes a request-scoped logger on the
// context, recovers panics, and logs one event per request. Requests that
// ended in a 5xx are logged at error level, 4xx at warn, everything else at
// info, so no failed request leaves the process unrecorded.
func Middleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set(RequestIDHeader, id)

		requestLogger := logger.With(
			slog.String("requestId", id),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)
		ctx := WithLogger(withRequestID(r.Context(), id), requestLogger)
		recorder := &responseRecorder{ResponseWriter: w, status: http.StatusOK}

		defer func() {
			if recovered := recover(); recovered != nil {
				// net/http would also abort the connection on a re-panic, but
				// it logs unstructured to stderr and the client gets nothing.
				requestLogger.Error("panic recovered",
					slog.Any("panic", recovered),
					slog.String("stack", string(debug.Stack())),
				)
				if !recorder.wrote {
					recorder.Header().Set("Content-Type", "application/json")
					recorder.WriteHeader(http.StatusInternalServerError)
					if _, err := recorder.Write([]byte(`{"error":"internal error"}`)); err != nil {
						requestLogger.Error("could not write panic response", slog.String("error", err.Error()))
					}
				}
			}
			logRequest(requestLogger, r, recorder, time.Since(start))
		}()

		next.ServeHTTP(recorder, r.WithContext(ctx))
	})
}

func logRequest(logger *slog.Logger, r *http.Request, recorder *responseRecorder, elapsed time.Duration) {
	attrs := []any{
		slog.Int("status", recorder.status),
		slog.Int64("bytes", recorder.written),
		slog.Duration("duration", elapsed),
		slog.String("remoteAddr", clientIP(r)),
	}
	if query := r.URL.RawQuery; query != "" {
		attrs = append(attrs, slog.String("query", query))
	}
	if agent := r.UserAgent(); agent != "" {
		attrs = append(attrs, slog.String("userAgent", agent))
	}
	switch {
	case recorder.status >= 500:
		logger.Error("request failed", attrs...)
	case recorder.status >= 400:
		logger.Warn("request rejected", attrs...)
	default:
		logger.Info("request", attrs...)
	}
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
