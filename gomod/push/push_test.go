package push

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

func TestPushSendsFilesInCorrectOrder(t *testing.T) {
	dir := t.TempDir()

	// Create test module files.
	files := map[string]string{
		"github.com/foo/bar/@v/v1.0.0.info": `{"Version":"v1.0.0"}`,
		"github.com/foo/bar/@v/v1.0.0.mod":  "module github.com/foo/bar\n",
		"github.com/foo/bar/@v/v1.0.0.zip":  "fake-zip",
	}
	for name, content := range files {
		fpath := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		if err := os.WriteFile(fpath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
	}

	var mu sync.Mutex
	var receivedPaths []string
	receivedBodies := make(map[string]string)
	var receivedAuth string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "expected PUT", http.StatusMethodNotAllowed)
			return
		}

		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedPaths = append(receivedPaths, r.URL.Path)
		receivedBodies[r.URL.Path] = string(body)
		receivedAuth = r.Header.Get("Authorization")
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	p := New(log, ts.URL)
	p.SetAuthToken("test-token")

	if err := p.Push(context.Background(), dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all files were sent.
	if len(receivedPaths) != 3 {
		t.Fatalf("got %d requests, expected 3", len(receivedPaths))
	}

	// Verify order: .info before .mod before .zip.
	var infoIdx, modIdx, zipIdx int
	for i, p := range receivedPaths {
		switch {
		case filepath.Ext(p) == ".info":
			infoIdx = i
		case filepath.Ext(p) == ".mod":
			modIdx = i
		case filepath.Ext(p) == ".zip":
			zipIdx = i
		}
	}
	if infoIdx >= modIdx {
		t.Errorf(".info (idx %d) should be sent before .mod (idx %d)", infoIdx, modIdx)
	}
	if modIdx >= zipIdx {
		t.Errorf(".mod (idx %d) should be sent before .zip (idx %d)", modIdx, zipIdx)
	}

	// Verify auth header.
	if receivedAuth != "Bearer test-token" {
		t.Errorf("got auth %q, expected %q", receivedAuth, "Bearer test-token")
	}

	// Verify content.
	if receivedBodies["/go/github.com/foo/bar/@v/v1.0.0.info"] != `{"Version":"v1.0.0"}` {
		t.Errorf("unexpected .info content: %q", receivedBodies["/go/github.com/foo/bar/@v/v1.0.0.info"])
	}
}

func TestPushReturnsErrorForEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	p := New(log, "http://localhost:9999")

	err := p.Push(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
}

func TestPushWalksNestedDirectories(t *testing.T) {
	dir := t.TempDir()

	// Create files for two modules.
	modules := map[string]string{
		"github.com/foo/bar/@v/v1.0.0.info": "info1",
		"github.com/foo/bar/@v/v1.0.0.mod":  "mod1",
		"github.com/foo/bar/@v/v1.0.0.zip":  "zip1",
		"github.com/baz/qux/@v/v2.0.0.info": "info2",
		"github.com/baz/qux/@v/v2.0.0.mod":  "mod2",
		"github.com/baz/qux/@v/v2.0.0.zip":  "zip2",
	}
	for name, content := range modules {
		fpath := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		if err := os.WriteFile(fpath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
	}

	var mu sync.Mutex
	var receivedPaths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedPaths = append(receivedPaths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	p := New(log, ts.URL)

	if err := p.Push(context.Background(), dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(receivedPaths) != 6 {
		sort.Strings(receivedPaths)
		t.Fatalf("got %d requests, expected 6: %v", len(receivedPaths), receivedPaths)
	}
}
