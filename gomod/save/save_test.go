package save

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/a-h/depot/storage"
)

func TestSaveFromGoMod(t *testing.T) {
	// Create a test go.mod file.
	dir := t.TempDir()
	goModPath := filepath.Join(dir, "go.mod")
	goModContent := `module example.com/myapp

go 1.21

require (
	github.com/foo/bar v1.0.0
	github.com/baz/qux v0.2.0
)
`
	if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	// Set up mock proxy that serves go.mod files with no transitive deps.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/github.com/foo/bar/@v/v1.0.0.info":
			w.Write([]byte(`{"Version":"v1.0.0","Time":"2024-01-01T00:00:00Z"}`))
		case "/github.com/foo/bar/@v/v1.0.0.mod":
			w.Write([]byte("module github.com/foo/bar\n\ngo 1.21\n"))
		case "/github.com/foo/bar/@v/v1.0.0.zip":
			w.Write([]byte("fake-zip"))
		case "/github.com/baz/qux/@v/v0.2.0.info":
			w.Write([]byte(`{"Version":"v0.2.0","Time":"2024-01-01T00:00:00Z"}`))
		case "/github.com/baz/qux/@v/v0.2.0.mod":
			w.Write([]byte("module github.com/baz/qux\n\ngo 1.21\n"))
		case "/github.com/baz/qux/@v/v0.2.0.zip":
			w.Write([]byte("fake-zip"))
		default:
			http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer ts.Close()

	storeDir := t.TempDir()
	s := storage.NewFileSystem(storeDir)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	saver := New(log, s)
	saver.SetProxyURL(ts.URL)

	if err := saver.Save(context.Background(), []string{goModPath}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify both modules were downloaded.
	for _, name := range []string{
		"github.com/foo/bar/@v/v1.0.0.zip",
		"github.com/baz/qux/@v/v0.2.0.zip",
	} {
		if _, err := os.Stat(filepath.Join(storeDir, name)); err != nil {
			t.Errorf("expected file %s to exist: %v", name, err)
		}
	}
}

func TestSaveResolvesTransitiveDependencies(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		// Root module depends on dep-a.
		case "/github.com/root/mod/@v/v1.0.0.info":
			w.Write([]byte(`{"Version":"v1.0.0","Time":"2024-01-01T00:00:00Z"}`))
		case "/github.com/root/mod/@v/v1.0.0.mod":
			w.Write([]byte("module github.com/root/mod\n\ngo 1.21\n\nrequire github.com/dep/a v1.0.0\n"))
		case "/github.com/root/mod/@v/v1.0.0.zip":
			w.Write([]byte("fake-zip"))
		// dep-a depends on dep-b.
		case "/github.com/dep/a/@v/v1.0.0.info":
			w.Write([]byte(`{"Version":"v1.0.0","Time":"2024-01-01T00:00:00Z"}`))
		case "/github.com/dep/a/@v/v1.0.0.mod":
			w.Write([]byte("module github.com/dep/a\n\ngo 1.21\n\nrequire github.com/dep/b v0.2.0\n"))
		case "/github.com/dep/a/@v/v1.0.0.zip":
			w.Write([]byte("fake-zip"))
		// dep-b is a leaf.
		case "/github.com/dep/b/@v/v0.2.0.info":
			w.Write([]byte(`{"Version":"v0.2.0","Time":"2024-01-01T00:00:00Z"}`))
		case "/github.com/dep/b/@v/v0.2.0.mod":
			w.Write([]byte("module github.com/dep/b\n\ngo 1.21\n"))
		case "/github.com/dep/b/@v/v0.2.0.zip":
			w.Write([]byte("fake-zip"))
		default:
			http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer ts.Close()

	storeDir := t.TempDir()
	s := storage.NewFileSystem(storeDir)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	saver := New(log, s)
	saver.SetProxyURL(ts.URL)

	if err := saver.Save(context.Background(), []string{"github.com/root/mod@v1.0.0"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All three modules should have been downloaded.
	for _, name := range []string{
		"github.com/root/mod/@v/v1.0.0.zip",
		"github.com/dep/a/@v/v1.0.0.zip",
		"github.com/dep/b/@v/v0.2.0.zip",
	} {
		if _, err := os.Stat(filepath.Join(storeDir, name)); err != nil {
			t.Errorf("expected file %s to exist: %v", name, err)
		}
	}
}

func TestSaveHandlesReplaceDirectives(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/github.com/root/mod/@v/v1.0.0.info":
			w.Write([]byte(`{"Version":"v1.0.0","Time":"2024-01-01T00:00:00Z"}`))
		case "/github.com/root/mod/@v/v1.0.0.mod":
			// Replace dep/a with dep/a-fork.
			w.Write([]byte("module github.com/root/mod\n\ngo 1.21\n\nrequire github.com/dep/a v1.0.0\n\nreplace github.com/dep/a => github.com/dep/a-fork v1.1.0\n"))
		case "/github.com/root/mod/@v/v1.0.0.zip":
			w.Write([]byte("fake-zip"))
		// The fork should be downloaded, not the original.
		case "/github.com/dep/a-fork/@v/v1.1.0.info":
			w.Write([]byte(`{"Version":"v1.1.0","Time":"2024-01-01T00:00:00Z"}`))
		case "/github.com/dep/a-fork/@v/v1.1.0.mod":
			w.Write([]byte("module github.com/dep/a-fork\n\ngo 1.21\n"))
		case "/github.com/dep/a-fork/@v/v1.1.0.zip":
			w.Write([]byte("fake-zip"))
		default:
			http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer ts.Close()

	storeDir := t.TempDir()
	s := storage.NewFileSystem(storeDir)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	saver := New(log, s)
	saver.SetProxyURL(ts.URL)

	if err := saver.Save(context.Background(), []string{"github.com/root/mod@v1.0.0"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The fork should be downloaded.
	if _, err := os.Stat(filepath.Join(storeDir, "github.com/dep/a-fork/@v/v1.1.0.zip")); err != nil {
		t.Error("expected a-fork to be downloaded")
	}

	// The original should not be downloaded.
	if _, err := os.Stat(filepath.Join(storeDir, "github.com/dep/a/@v/v1.0.0.zip")); err == nil {
		t.Error("original dep/a should not have been downloaded")
	}
}

func TestSaveResolvesLatestVersion(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/github.com/foo/bar/@latest":
			w.Write([]byte(`{"Version":"v3.0.0","Time":"2024-06-01T00:00:00Z"}`))
		case "/github.com/foo/bar/@v/v3.0.0.info":
			w.Write([]byte(`{"Version":"v3.0.0","Time":"2024-06-01T00:00:00Z"}`))
		case "/github.com/foo/bar/@v/v3.0.0.mod":
			w.Write([]byte("module github.com/foo/bar\n\ngo 1.21\n"))
		case "/github.com/foo/bar/@v/v3.0.0.zip":
			w.Write([]byte("fake-zip"))
		default:
			http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer ts.Close()

	storeDir := t.TempDir()
	s := storage.NewFileSystem(storeDir)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	saver := New(log, s)
	saver.SetProxyURL(ts.URL)

	// No version specified -- should resolve latest.
	if err := saver.Save(context.Background(), []string{"github.com/foo/bar"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(storeDir, "github.com/foo/bar/@v/v3.0.0.zip")); err != nil {
		t.Error("expected v3.0.0 to be downloaded after resolving latest")
	}
}
