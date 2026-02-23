package nixcacheinfo

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nix-community/go-nix/pkg/narinfo/signature"
)

func TestHandler(t *testing.T) {
	log := slog.New(slog.DiscardHandler)

	t.Run("cache info is returned", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/nix-cache-info", nil)
		w := httptest.NewRecorder()

		h := New(log, nil)
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status code %d, got %d with body:\n%s", http.StatusOK, w.Code, w.Body.String())
		}
		if w.Body.String() != CacheInfo {
			t.Fatalf("expected body:\n%s\ngot:\n%s", CacheInfo, w.Body.String())
		}
	})
	t.Run("public key is included if private key is provided", func(t *testing.T) {
		privateKey, publicKey, err := signature.GenerateKeypair("test-key", nil)
		if err != nil {
			t.Fatalf("failed to generate keypair: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/nix-cache-info", nil)
		w := httptest.NewRecorder()

		h := New(log, &privateKey)
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status code %d, got %d with body:\n%s", http.StatusOK, w.Code, w.Body.String())
		}

		expected := CacheInfo + "PublicKey: " + publicKey.String() + "\n"
		if w.Body.String() != expected {
			t.Fatalf("expected body:\n%s\ngot:\n%s", expected, w.Body.String())
		}
	})
}
