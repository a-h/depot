package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-h/depot/gomod/db"
	gomodhandler "github.com/a-h/depot/gomod/handlers"
	"github.com/a-h/depot/gomod/push"
	"github.com/a-h/depot/gomod/save"
	"github.com/a-h/depot/metrics"
	"github.com/a-h/depot/storage"
	"github.com/a-h/depot/store"
)

// TestEndToEnd downloads a real module from proxy.golang.org, pushes it to a
// depot server, then creates a Go project that fetches the module from depot.
// It validates that the Go toolchain can resolve both direct and transitive
// dependencies through the depot proxy.
func TestEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Check that proxy.golang.org is reachable.
	resp, err := http.Get("https://proxy.golang.org/rsc.io/quote/@latest")
	if err != nil {
		t.Skipf("skipping: proxy.golang.org is not reachable: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("skipping: proxy.golang.org returned %d", resp.StatusCode)
	}

	// Check that the go binary is available.
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("skipping: go binary not found: %v", err)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := context.Background()

	// Step 1: Save rsc.io/quote@v1.5.2 and its transitive dependencies.
	saveDir := t.TempDir()
	saveStorage := storage.NewFileSystem(saveDir)
	saver := save.New(log, saveStorage)

	t.Log("saving rsc.io/quote@v1.5.2 from proxy.golang.org")
	if err := saver.Save(ctx, []string{"rsc.io/quote@v1.5.2"}); err != nil {
		t.Fatalf("failed to save module: %v", err)
	}

	// Verify that transitive deps were saved.
	expectedModules := []string{
		"rsc.io/quote/@v/v1.5.2.zip",
		"rsc.io/sampler/@v/v1.3.0.zip",
	}
	for _, m := range expectedModules {
		if _, err := os.Stat(filepath.Join(saveDir, m)); err != nil {
			t.Fatalf("expected saved file %s to exist: %v", m, err)
		}
	}
	t.Log("save complete, transitive dependencies resolved")

	// Step 2: Start a depot server.
	kvStore, closer, err := store.New(ctx, "sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer closer()
	if err := kvStore.Init(ctx); err != nil {
		t.Fatalf("failed to init store: %v", err)
	}

	m, err := metrics.New()
	if err != nil {
		t.Fatalf("failed to create metrics: %v", err)
	}

	serverStorageDir := t.TempDir()
	serverStorage := storage.NewFileSystem(serverStorageDir)
	goDb := db.New(kvStore)
	handler := gomodhandler.New(log, goDb, serverStorage, m)
	mux := http.NewServeMux()
	mux.Handle("/go/", http.StripPrefix("/go", handler))
	server := httptest.NewServer(mux)
	defer server.Close()
	t.Logf("depot server listening at %s", server.URL)

	// Step 3: Push saved modules to the depot server.
	pusher := push.New(log, server.URL)
	if err := pusher.Push(ctx, saveDir); err != nil {
		t.Fatalf("failed to push modules: %v", err)
	}
	t.Log("push complete")

	// Verify the proxy protocol endpoints work.
	t.Run("proxy protocol endpoints return correct data", func(t *testing.T) {
		// /@v/list should contain v1.5.2.
		resp, err := http.Get(server.URL + "/go/rsc.io/quote/@v/list")
		if err != nil {
			t.Fatalf("GET list failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET list got %d", resp.StatusCode)
		}

		// /@latest should return v1.5.2.
		resp, err = http.Get(server.URL + "/go/rsc.io/quote/@latest")
		if err != nil {
			t.Fatalf("GET @latest failed: %v", err)
		}
		defer resp.Body.Close()
		var info db.VersionInfo
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			t.Fatalf("failed to decode @latest: %v", err)
		}
		if info.Version != "v1.5.2" {
			t.Errorf("got latest version %q, expected %q", info.Version, "v1.5.2")
		}
	})

	// Step 4: Create a Go project that imports rsc.io/quote, then resolve deps from depot.
	t.Run("go mod download resolves modules from depot", func(t *testing.T) {
		projectDir := t.TempDir()

		goModContent := fmt.Sprintf(`module testproject

go 1.21

require rsc.io/quote v1.5.2
`)
		if err := os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte(goModContent), 0644); err != nil {
			t.Fatalf("failed to write go.mod: %v", err)
		}

		mainContent := `package main

import (
	"fmt"
	"rsc.io/quote"
)

func main() {
	fmt.Println(quote.Hello())
}
`
		if err := os.WriteFile(filepath.Join(projectDir, "main.go"), []byte(mainContent), 0644); err != nil {
			t.Fatalf("failed to write main.go: %v", err)
		}

		// Use a clean GOPATH so we don't use any cached modules.
		gopath := t.TempDir()

		// Run go mod download with GOPROXY pointing at our depot server.
		// No ",direct" fallback -- depot must serve everything.
		cmd := exec.Command(goBin, "mod", "download", "-x", "all")
		cmd.Dir = projectDir
		cmd.Env = append(os.Environ(),
			"GOPROXY="+server.URL+"/go",
			"GONOSUMCHECK=*",
			"GONOSUMDB=*",
			"GOPATH="+gopath,
			"GOFLAGS=-modcacherw",
		)
		output, err := cmd.CombinedOutput()
		t.Logf("go mod download output:\n%s", string(output))
		if err != nil {
			t.Fatalf("go mod download failed: %v", err)
		}

		// Verify that modules were downloaded to the local cache.
		cmd = exec.Command(goBin, "list", "-m", "all")
		cmd.Dir = projectDir
		cmd.Env = append(os.Environ(),
			"GOPROXY="+server.URL+"/go",
			"GONOSUMCHECK=*",
			"GONOSUMDB=*",
			"GOPATH="+gopath,
		)
		output, err = cmd.CombinedOutput()
		t.Logf("go list -m all output:\n%s", string(output))
		if err != nil {
			t.Fatalf("go list -m all failed: %v", err)
		}

		modules := string(output)
		for _, expected := range []string{"rsc.io/quote", "rsc.io/sampler"} {
			if !strings.Contains(modules, expected) {
				t.Errorf("expected %q in module list, got:\n%s", expected, modules)
			}
		}
	})
}
