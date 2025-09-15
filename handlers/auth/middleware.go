package auth

import (
	"log/slog"
	"net/http"
	"strings"
)

type Middleware struct {
	log   *slog.Logger
	token string
	next  http.Handler
}

func NewMiddleware(log *slog.Logger, token string, next http.Handler) *Middleware {
	return &Middleware{
		log:   log,
		token: token,
		next:  next,
	}
}

func (m *Middleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only require authentication for write operations
	if r.Method != http.MethodPut && r.Method != http.MethodPost && r.Method != http.MethodDelete {
		m.next.ServeHTTP(w, r)
		return
	}

	// If no token is configured, allow all writes (for backward compatibility)
	if m.token == "" {
		m.log.Warn("no upload token configured - uploads are not protected")
		m.next.ServeHTTP(w, r)
		return
	}

	// Check for Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		m.log.Warn("upload attempt without authorization header", slog.String("method", r.Method), slog.String("path", r.URL.Path))
		http.Error(w, "Authorization required for uploads", http.StatusUnauthorized)
		return
	}

	// Support both "Bearer <token>" and just "<token>" formats
	token := authHeader
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = strings.TrimPrefix(authHeader, "Bearer ")
	}

	if token != m.token {
		m.log.Warn("upload attempt with invalid token", slog.String("method", r.Method), slog.String("path", r.URL.Path))
		http.Error(w, "Invalid authorization token", http.StatusUnauthorized)
		return
	}

	m.log.Info("authorized upload", slog.String("method", r.Method), slog.String("path", r.URL.Path))
	m.next.ServeHTTP(w, r)
}
