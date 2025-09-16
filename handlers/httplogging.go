package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type Logger struct {
	log  *slog.Logger
	next http.Handler
}

func NewLogger(log *slog.Logger, next http.Handler) *Logger {
	return &Logger{
		log:  log,
		next: next,
	}
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status        int
	size          int
	headerWritten bool
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	if lrw.headerWritten {
		return
	}
	lrw.status = code
	lrw.headerWritten = true
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	if lrw.status == 0 {
		lrw.status = http.StatusOK
	}
	n, err := lrw.ResponseWriter.Write(b)
	lrw.size += n
	return n, err
}

func (l *Logger) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	msg := fmt.Sprintf("%s %s", r.Method, r.URL.Path)

	lrw := &loggingResponseWriter{
		ResponseWriter: w,
	}

	defer func() {
		dur := time.Since(start).Milliseconds()
		if rec := recover(); rec != nil {
			l.log.Error(msg, slog.Any("panic", rec), slog.Int("status", http.StatusInternalServerError), slog.Int64("ms", dur))
			if !lrw.headerWritten {
				http.Error(lrw, "internal server error", http.StatusInternalServerError)
			}
			return
		}
		l.log.Info(msg, slog.Int("status", lrw.status), slog.Int("bytes", lrw.size), slog.Int64("ms", dur))
	}()

	l.next.ServeHTTP(lrw, r)
}
