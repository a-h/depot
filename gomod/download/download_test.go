package download

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/a-h/depot/storage"
)

func TestParseModuleSpec(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedPath    string
		expectedVersion string
	}{
		{
			name:            "module with version is parsed",
			input:           "github.com/foo/bar@v1.0.0",
			expectedPath:    "github.com/foo/bar",
			expectedVersion: "v1.0.0",
		},
		{
			name:            "module without version is parsed",
			input:           "github.com/foo/bar",
			expectedPath:    "github.com/foo/bar",
			expectedVersion: "",
		},
		{
			name:            "module with v2 suffix and version is parsed",
			input:           "github.com/foo/bar/v2@v2.1.0",
			expectedPath:    "github.com/foo/bar/v2",
			expectedVersion: "v2.1.0",
		},
		{
			name:            "module with incompatible version is parsed",
			input:           "github.com/Azure/go-autorest@v14.2.0+incompatible",
			expectedPath:    "github.com/Azure/go-autorest",
			expectedVersion: "v14.2.0+incompatible",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseModuleSpec(tt.input)
			if got.Path != tt.expectedPath {
				t.Errorf("got path %q, expected %q", got.Path, tt.expectedPath)
			}
			if got.Version != tt.expectedVersion {
				t.Errorf("got version %q, expected %q", got.Version, tt.expectedVersion)
			}
		})
	}
}

func TestDownloadSkipsExistingFiles(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewFileSystem(dir)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Pre-create the files.
	ctx := context.Background()
	for _, name := range []string{
		"github.com/foo/bar/@v/v1.0.0.info",
		"github.com/foo/bar/@v/v1.0.0.mod",
		"github.com/foo/bar/@v/v1.0.0.zip",
	} {
		w, err := s.Put(ctx, name)
		if err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		w.Write([]byte("existing content"))
		w.Close()
	}

	// Start a server that should not be called.
	var called bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer ts.Close()

	d := New(log, s)
	d.SetProxyURL(ts.URL)

	content, err := d.Download(ctx, "github.com/foo/bar", "v1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("upstream proxy should not have been called for existing files")
	}
	if string(content) != "existing content" {
		t.Errorf("got content %q, expected %q", string(content), "existing content")
	}
}

func TestResolveLatest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/github.com/foo/bar/@latest" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"Version": "v1.2.3"})
	}))
	defer ts.Close()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	d := New(log, storage.NewFileSystem(t.TempDir()))
	d.SetProxyURL(ts.URL)

	version, err := d.ResolveLatest(context.Background(), "github.com/foo/bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "v1.2.3" {
		t.Errorf("got version %q, expected %q", version, "v1.2.3")
	}
}

func TestDownload(t *testing.T) {
	goModContent := "module github.com/foo/bar\n\ngo 1.21\n"
	infoContent := `{"Version":"v1.0.0","Time":"2024-01-01T00:00:00Z"}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/github.com/foo/bar/@v/v1.0.0.info":
			w.Write([]byte(infoContent))
		case "/github.com/foo/bar/@v/v1.0.0.mod":
			w.Write([]byte(goModContent))
		case "/github.com/foo/bar/@v/v1.0.0.zip":
			w.Write([]byte("fake-zip-data"))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	dir := t.TempDir()
	s := storage.NewFileSystem(dir)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	d := New(log, s)
	d.SetProxyURL(ts.URL)

	content, err := d.Download(context.Background(), "github.com/foo/bar", "v1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != goModContent {
		t.Errorf("got go.mod content %q, expected %q", string(content), goModContent)
	}

	// Verify files were stored.
	for _, name := range []string{
		"github.com/foo/bar/@v/v1.0.0.info",
		"github.com/foo/bar/@v/v1.0.0.mod",
		"github.com/foo/bar/@v/v1.0.0.zip",
	} {
		fpath := filepath.Join(dir, name)
		if _, err := os.Stat(fpath); err != nil {
			t.Errorf("expected file %s to exist: %v", name, err)
		}
	}
}
