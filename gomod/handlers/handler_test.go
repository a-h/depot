package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/a-h/depot/gomod/db"
	"github.com/a-h/depot/metrics"
	"github.com/a-h/depot/storage"
	"github.com/a-h/depot/store"
)

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	s, closer, err := store.New(context.Background(), "sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { closer() })
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	m, err := metrics.New()
	if err != nil {
		t.Fatalf("failed to create metrics: %v", err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return New(log, db.New(s), storage.NewFileSystem(t.TempDir()), m)
}

func TestParsePath(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		expectedModule string
		expectedRes    string
		expectError    bool
	}{
		{
			name:           "version list is parsed",
			path:           "/github.com/foo/bar/@v/list",
			expectedModule: "github.com/foo/bar",
			expectedRes:    "list",
		},
		{
			name:           "version info is parsed",
			path:           "/github.com/foo/bar/@v/v1.0.0.info",
			expectedModule: "github.com/foo/bar",
			expectedRes:    "v1.0.0.info",
		},
		{
			name:           "version mod is parsed",
			path:           "/github.com/foo/bar/@v/v1.0.0.mod",
			expectedModule: "github.com/foo/bar",
			expectedRes:    "v1.0.0.mod",
		},
		{
			name:           "version zip is parsed",
			path:           "/github.com/foo/bar/@v/v1.0.0.zip",
			expectedModule: "github.com/foo/bar",
			expectedRes:    "v1.0.0.zip",
		},
		{
			name:           "latest is parsed",
			path:           "/github.com/foo/bar/@latest",
			expectedModule: "github.com/foo/bar",
			expectedRes:    "@latest",
		},
		{
			name:           "encoded capital letters are decoded",
			path:           "/github.com/!azure/go-autorest/@v/list",
			expectedModule: "github.com/Azure/go-autorest",
			expectedRes:    "list",
		},
		{
			name:           "nested module path is parsed",
			path:           "/github.com/foo/bar/v2/@v/v2.0.0.info",
			expectedModule: "github.com/foo/bar/v2",
			expectedRes:    "v2.0.0.info",
		},
		{
			name:        "missing separator returns error",
			path:        "/github.com/foo/bar",
			expectError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := parsePath(tt.path)
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.modulePath != tt.expectedModule {
				t.Errorf("got module %q, expected %q", info.modulePath, tt.expectedModule)
			}
			if info.resource != tt.expectedRes {
				t.Errorf("got resource %q, expected %q", info.resource, tt.expectedRes)
			}
		})
	}
}

func TestGetReturnsNotFoundForMissingModule(t *testing.T) {
	h := newTestHandler(t)

	paths := []string{
		"/github.com/foo/bar/@v/list",
		"/github.com/foo/bar/@v/v1.0.0.info",
		"/github.com/foo/bar/@v/v1.0.0.mod",
		"/github.com/foo/bar/@v/v1.0.0.zip",
		"/github.com/foo/bar/@latest",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, p, nil)
			h.ServeHTTP(rr, req)

			// list returns 200 with empty body, others return 404.
			if p == "/github.com/foo/bar/@v/list" {
				if rr.Code != http.StatusOK {
					t.Errorf("got status %d, expected %d", rr.Code, http.StatusOK)
				}
				if rr.Body.Len() != 0 {
					t.Errorf("expected empty body, got %q", rr.Body.String())
				}
			} else {
				if rr.Code != http.StatusNotFound {
					t.Errorf("got status %d, expected %d for %s", rr.Code, http.StatusNotFound, p)
				}
			}
		})
	}
}

func TestPutThenGetRoundTrip(t *testing.T) {
	h := newTestHandler(t)

	// PUT .info.
	infoBody := `{"Version":"v1.0.0","Time":"2024-01-01T00:00:00Z"}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/github.com/foo/bar/@v/v1.0.0.info", bytes.NewBufferString(infoBody))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT .info got status %d: %s", rr.Code, rr.Body.String())
	}

	// PUT .mod.
	modBody := "module github.com/foo/bar\n\ngo 1.21\n"
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/github.com/foo/bar/@v/v1.0.0.mod", bytes.NewBufferString(modBody))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT .mod got status %d: %s", rr.Code, rr.Body.String())
	}

	// PUT .zip.
	zipBody := "fake-zip-content"
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/github.com/foo/bar/@v/v1.0.0.zip", bytes.NewBufferString(zipBody))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT .zip got status %d: %s", rr.Code, rr.Body.String())
	}

	// GET .info.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/github.com/foo/bar/@v/v1.0.0.info", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET .info got status %d", rr.Code)
	}
	var gotInfo db.VersionInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &gotInfo); err != nil {
		t.Fatalf("failed to decode .info response: %v", err)
	}
	if gotInfo.Version != "v1.0.0" {
		t.Errorf("got version %q, expected %q", gotInfo.Version, "v1.0.0")
	}

	// GET .mod.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/github.com/foo/bar/@v/v1.0.0.mod", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET .mod got status %d", rr.Code)
	}
	if rr.Body.String() != modBody {
		t.Errorf("got mod %q, expected %q", rr.Body.String(), modBody)
	}

	// GET .zip.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/github.com/foo/bar/@v/v1.0.0.zip", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET .zip got status %d", rr.Code)
	}
	if rr.Body.String() != zipBody {
		t.Errorf("got zip %q, expected %q", rr.Body.String(), zipBody)
	}

	// GET list.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/github.com/foo/bar/@v/list", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET list got status %d", rr.Code)
	}
	body, _ := io.ReadAll(rr.Body)
	if got := string(bytes.TrimSpace(body)); got != "v1.0.0" {
		t.Errorf("got list %q, expected %q", got, "v1.0.0")
	}

	// GET @latest.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/github.com/foo/bar/@latest", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET @latest got status %d", rr.Code)
	}
	var latestInfo db.VersionInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &latestInfo); err != nil {
		t.Fatalf("failed to decode @latest response: %v", err)
	}
	if latestInfo.Version != "v1.0.0" {
		t.Errorf("got latest version %q, expected %q", latestInfo.Version, "v1.0.0")
	}
}
