package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/a-h/depot/auth"
	"golang.org/x/crypto/ssh"
)

type Middleware struct {
	log        *slog.Logger
	authConfig *auth.AuthConfig
	next       http.Handler
}

func NewMiddleware(log *slog.Logger, authConfig *auth.AuthConfig, next http.Handler) *Middleware {
	if authConfig == nil || len(authConfig.Keys) == 0 {
		log.Warn("no authentication configured - all access is permitted")
	}
	return &Middleware{
		log:        log,
		authConfig: authConfig,
		next:       next,
	}
}

func (m *Middleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// If no auth config, allow all access.
	if m.authConfig == nil || len(m.authConfig.Keys) == 0 {
		m.next.ServeHTTP(w, r)
		return
	}

	isWriteOperation := r.Method == http.MethodPut || r.Method == http.MethodPost || r.Method == http.MethodDelete

	// Check if authentication is required for read operations.
	if !isWriteOperation && !m.authConfig.RequireAuthForRead {
		m.next.ServeHTTP(w, r)
		return
	}

	// Check for Authorization header.
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		operation := "read"
		if isWriteOperation {
			operation = "write"
		}
		m.log.Warn("request without authorization header", slog.String("operation", operation), slog.String("method", r.Method), slog.String("path", r.URL.Path))
		http.Error(w, "Authorization required", http.StatusUnauthorized)
		return
	}

	// Extract JWT token from Bearer header.
	token := authHeader
	if strings.HasPrefix(token, "Bearer ") {
		token = strings.TrimPrefix(token, "Bearer ")
	}

	// Verify JWT token.
	keyFingerprint, err := auth.VerifyJWT(token, m.authConfig)
	if err != nil {
		m.log.Warn("invalid JWT token", slog.String("error", err.Error()), slog.String("method", r.Method), slog.String("path", r.URL.Path))
		http.Error(w, "Invalid authorization token", http.StatusUnauthorized)
		return
	}

	// Find the authorized key to check permissions.
	var authorizedKey *auth.AuthorizedKey
	for _, key := range m.authConfig.Keys {
		if ssh.FingerprintSHA256(key.PublicKey) == keyFingerprint {
			authorizedKey = &key
			break
		}
	}
	if authorizedKey == nil {
		m.log.Warn("key not found in auth config", slog.String("fingerprint", keyFingerprint))
		http.Error(w, "Invalid authorization token", http.StatusUnauthorized)
		return
	}

	// Check permissions.
	if isWriteOperation && authorizedKey.Permission != auth.PermissionReadWrite {
		m.log.Warn("insufficient permissions for write operation", slog.String("fingerprint", keyFingerprint), slog.String("permission", string(authorizedKey.Permission)))
		http.Error(w, "Insufficient permissions", http.StatusForbidden)
		return
	}

	m.log.Debug("authorized request", slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.String("fingerprint", keyFingerprint), slog.String("permission", string(authorizedKey.Permission)))
	m.next.ServeHTTP(w, r)
}
