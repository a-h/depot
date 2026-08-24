package auth_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/depot/auth"
	authmiddleware "github.com/a-h/depot/middleware/auth"
	"golang.org/x/crypto/ssh"
)

func TestRequestsAreAuthorizedAgainstTheAuthFile(t *testing.T) {
	roleToSigner := map[string]ssh.Signer{}
	roleToToken := map[string]string{}
	for _, role := range []string{"reader", "writer", "unknown"} {
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("failed to generate %s key: %v", role, err)
		}
		signer, err := ssh.NewSignerFromKey(key)
		if err != nil {
			t.Fatalf("failed to create %s signer: %v", role, err)
		}
		token, err := auth.CreateJWT(signer)
		if err != nil {
			t.Fatalf("failed to create %s token: %v", role, err)
		}
		roleToSigner[role], roleToToken[role] = signer, token
	}

	writeOnly := &auth.AuthConfig{
		Keys: []auth.AuthorizedKey{{Permission: auth.PermissionReadWrite, PublicKey: roleToSigner["writer"].PublicKey()}},
	}
	readAndWrite := &auth.AuthConfig{
		Keys: []auth.AuthorizedKey{
			{Permission: auth.PermissionRead, PublicKey: roleToSigner["reader"].PublicKey()},
			{Permission: auth.PermissionReadWrite, PublicKey: roleToSigner["writer"].PublicKey()},
		},
		RequireAuthForRead: true,
	}

	tests := []struct {
		name     string
		config   *auth.AuthConfig
		method   string
		token    string
		expected int
	}{
		{name: "without an auth file, reads are permitted", config: nil, method: http.MethodGet, expected: http.StatusNoContent},
		{name: "without an auth file, uploads are permitted", config: nil, method: http.MethodPut, expected: http.StatusNoContent},
		{name: "without an auth file, posts are permitted", config: nil, method: http.MethodPost, expected: http.StatusNoContent},
		{name: "without an auth file, deletes are permitted", config: nil, method: http.MethodDelete, expected: http.StatusNoContent},
		{name: "with an auth file that has no keys, uploads are permitted", config: &auth.AuthConfig{}, method: http.MethodPut, expected: http.StatusNoContent},

		{name: "with only write keys, unauthenticated reads are permitted", config: writeOnly, method: http.MethodGet, expected: http.StatusNoContent},
		{name: "with only write keys, unauthenticated uploads are rejected", config: writeOnly, method: http.MethodPut, expected: http.StatusUnauthorized},

		{name: "with a read key, unauthenticated reads are rejected", config: readAndWrite, method: http.MethodGet, expected: http.StatusUnauthorized},
		{name: "read-only keys can read", config: readAndWrite, method: http.MethodGet, token: roleToToken["reader"], expected: http.StatusNoContent},
		{name: "read-only keys cannot upload", config: readAndWrite, method: http.MethodPut, token: roleToToken["reader"], expected: http.StatusForbidden},
		{name: "read-only keys cannot delete", config: readAndWrite, method: http.MethodDelete, token: roleToToken["reader"], expected: http.StatusForbidden},
		{name: "read-write keys can read", config: readAndWrite, method: http.MethodGet, token: roleToToken["writer"], expected: http.StatusNoContent},
		{name: "read-write keys can upload", config: readAndWrite, method: http.MethodPut, token: roleToToken["writer"], expected: http.StatusNoContent},

		{name: "keys that are not in the auth file cannot read", config: readAndWrite, method: http.MethodGet, token: roleToToken["unknown"], expected: http.StatusUnauthorized},
		{name: "keys that are not in the auth file cannot upload", config: readAndWrite, method: http.MethodPut, token: roleToToken["unknown"], expected: http.StatusUnauthorized},
		{name: "tokens that are not JWTs are rejected", config: readAndWrite, method: http.MethodPut, token: "not-a-token", expected: http.StatusUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			handler := authmiddleware.New(log, test.config, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))

			r := httptest.NewRequest(test.method, "/nix/nar/test.nar", strings.NewReader("nar"))
			if test.token != "" {
				r.Header.Set("Authorization", "Bearer "+test.token)
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, r)

			if w.Code != test.expected {
				t.Errorf("expected status %d, got %d", test.expected, w.Code)
			}
		})
	}
}
