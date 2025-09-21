package narinfo

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "embed"

	"github.com/a-h/depot/nix/db"
	"github.com/a-h/depot/store"
)

//go:embed testdata/16hvpw4b3r05girazh4rnwbw0jgjkb4l.narinfo
var libGCCNarInfo string

func TestHandler(t *testing.T) {
	handlerLog := new(bytes.Buffer)
	log := slog.New(slog.NewJSONHandler(handlerLog, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := context.Background()
	store, closer, err := store.New(ctx, "sqlite", "file::memory:?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer closer()

	h := New(log, db.New(store), nil)

	t.Run("Get returns 404 if narinfo not found", func(t *testing.T) {
		r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/16hvpw4b3r05girazh4rnwbw0jgjkb4l.narinfo", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected status code %d, got %d with body:\n%s", http.StatusNotFound, w.Code, w.Body.String())
		}
	})
	t.Run("Put stores narinfo", func(t *testing.T) {
		r := httptest.NewRequestWithContext(ctx, http.MethodPut, "/16hvpw4b3r05girazh4rnwbw0jgjkb4l.narinfo", strings.NewReader(libGCCNarInfo))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected status code %d, got %d with logs:\n%s\nbody:\n%s", http.StatusCreated, w.Code, handlerLog.String(), w.Body.String())
		}
	})
	t.Run("Get retrieves stored narinfo", func(t *testing.T) {
		r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/16hvpw4b3r05girazh4rnwbw0jgjkb4l.narinfo", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status code %d, got %d with body:\n%s", http.StatusOK, w.Code, w.Body.String())
		}
		if w.Body.String() != libGCCNarInfo {
			t.Fatalf("expected body not found, got:\n%s", w.Body.String())
		}
	})
}
