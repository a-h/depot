package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a-h/depot/downloadcounter"
	"github.com/a-h/depot/metrics"
	"github.com/a-h/depot/nix/db"
	"github.com/a-h/depot/nix/handlers"
	"github.com/a-h/depot/storage"
	"github.com/a-h/depot/store"
	"github.com/nix-community/go-nix/pkg/nar"
	"github.com/nix-community/go-nix/pkg/narinfo"
	"github.com/nix-community/go-nix/pkg/narinfo/signature"
	"github.com/ulikunitz/xz"
)

//go:embed testdata/sl-aarch64-darwin.narinfo
var slAarch64DarwinNarinfo string

//go:embed testdata/sl-x86_64-linux.narinfo
var slX8664LinuxNarinfo string

const (
	depotURL = "http://localhost:8080"
	testPkg  = "github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl"

	// Test signing keys - these are test keys only, safe to commit to repo.
	testPrivateKey = "depot-test-1:I9FcLfz77TAEhqkIbQvPq3ecVn8A4Eml8SBek3Vk6TgBsla08REN3RYddk6pSEkfW1LBcgY7ln3aSbdupWF/+Q=="
	testPublicKey  = "depot-test-1:AbJWtPERDd0WHXZOqUhJH1tSwXIGO5Z92km3bqVhf/k="
)

// threadSafeWriter provides a thread-safe writer with mutex protection.
type threadSafeWriter struct {
	buf *bytes.Buffer
	mu  sync.Mutex
}

func newThreadSafeWriter() *threadSafeWriter {
	return &threadSafeWriter{
		buf: new(bytes.Buffer),
	}
}

func (w *threadSafeWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *threadSafeWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func (w *threadSafeWriter) Len() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Len()
}

// getExpectedNarInfo returns the expected narinfo for the current architecture.
func getExpectedNarInfo(t *testing.T) *narinfo.NarInfo {
	var narinfoContent string
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		narinfoContent = slAarch64DarwinNarinfo
	case "linux/amd64":
		narinfoContent = slX8664LinuxNarinfo
	default:
		t.Skipf("Test not supported on %s/%s architecture", runtime.GOOS, runtime.GOARCH)
		return nil
	}

	// Parse the narinfo.
	expectedNarInfo, err := narinfo.Parse(strings.NewReader(narinfoContent))
	if err != nil {
		t.Fatalf("failed to parse expected narinfo: %v", err)
	}

	return expectedNarInfo
}

type testServer struct {
	server     *http.Server
	serverLogs *bytes.Buffer
	nixLogs    *threadSafeWriter
	tempDir    string
	done       chan struct{}
	started    chan struct{}
}

func (ts *testServer) start(t *testing.T) {
	var err error
	ts.tempDir, err = os.MkdirTemp("", "depot-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	ts.done = make(chan struct{})
	ts.started = make(chan struct{})
	ts.nixLogs = newThreadSafeWriter()

	storePath := filepath.Join(ts.tempDir, "store")
	storage := storage.NewFileSystem(storePath)

	// Initialize database and handlers.
	sqliteDBPath := fmt.Sprintf("file:%s?mode=rwc", filepath.Join(ts.tempDir, "depot.db"))
	store, closer, err := store.New(t.Context(), "sqlite", sqliteDBPath)
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}

	ts.serverLogs = new(bytes.Buffer)
	log := slog.New(slog.NewJSONHandler(ts.serverLogs, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	// Load test private key for signing.
	privateKey, err := signature.LoadSecretKey(testPrivateKey)
	if err != nil {
		t.Fatalf("failed to load test private key: %v", err)
	}

	// Create HTTP server.
	ts.server = &http.Server{
		Addr:    ":8080",
		Handler: handlers.New(log, db.New(store), storage, &privateKey, make(chan downloadcounter.DownloadEvent, 1), metrics.Metrics{}),
	}

	// Start server in goroutine.
	go func() {
		defer closer()
		close(ts.started)
		if err := ts.server.ListenAndServe(); err != http.ErrServerClosed {
			t.Errorf("server error: %v", err)
		}
		close(ts.done)
	}()

	// Wait for server to start.
	<-ts.started

	// Wait for server to be ready to accept connections.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("depot server failed to start within timeout")
		default:
			if resp, err := http.Get(depotURL + "/nix-cache-info"); err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (ts *testServer) stop(t *testing.T) {
	if ts.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ts.server.Shutdown(ctx)
		<-ts.done
	}
	if ts.tempDir != "" {
		os.RemoveAll(ts.tempDir)
	}

	// Print logs only if the test failed.
	if t.Failed() {
		if ts.serverLogs != nil && ts.serverLogs.Len() > 0 {
			t.Logf("Server logs (test failed):\n%s", ts.serverLogs.String())
		}
		if ts.nixLogs != nil && ts.nixLogs.Len() > 0 {
			t.Logf("Nix logs (test failed):\n%s", ts.nixLogs.String())
		}
	}
}

func (ts *testServer) get(urlPath string) (resp *http.Response, err error) {
	resp, err = http.Get(strings.TrimSuffix(depotURL, "/") + "/" + strings.TrimPrefix(urlPath, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to GET %s: %v", urlPath, err)
	}
	return resp, err
}

func (ts *testServer) getNarInfo(hashPart string) (*narinfo.NarInfo, error) {
	urlPath := fmt.Sprintf("%s.narinfo", hashPart)
	resp, err := ts.get(urlPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned status %d, expected 200", urlPath, resp.StatusCode)
	}

	info, err := narinfo.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse narinfo: %v", err)
	}
	return info, nil
}

func (ts *testServer) getSHA256(urlPath, expectedHash string) (err error) {
	resp, err := ts.get(urlPath)
	if err != nil {
		return fmt.Errorf("failed to GET %s: %v", urlPath, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned status %d, expected 200", urlPath, resp.StatusCode)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, resp.Body); err != nil {
		return fmt.Errorf("failed to read response body: %v", err)
	}
	actualHash := fmt.Sprintf("%x", hasher.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("hash mismatch for %s: expected %s, got %s", urlPath, expectedHash, actualHash)
	}
	return nil
}

func TestDepotIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping derivation push test in short mode")
		return
	}

	server := &testServer{}
	server.start(t)
	t.Cleanup(func() { server.stop(t) })

	slPath, err := Eval(server.nixLogs, server.nixLogs, testPkg)
	if err != nil {
		t.Fatalf("failed to evaluate sl package: %v", err)
	}

	drvPath, err := Eval(server.nixLogs, server.nixLogs, testPkg+".drvPath")
	if err != nil {
		t.Fatalf("failed to evaluate sl derivation: %v", err)
	}

	// Shared variables for subtests.
	hashPart := filepath.Base(slPath)[:32]
	expectedFlakeStorePath := "/nix/store/mg5riyrz6hva7njw82gr5ghvajklkccq-source"

	t.Run("packages can be pushed and verified", func(t *testing.T) {
		// Copy the package to depot.
		if err := CopyTo(server.nixLogs, server.nixLogs, ".", depotURL, false, slPath); err != nil {
			t.Fatalf("failed to copy sl package to depot: %v", err)
		}

		// Verify the package's narinfo is accessible and contains correct data.
		actualNarInfo, err := server.getNarInfo(hashPart)
		if err != nil {
			t.Fatalf("failed to get narinfo: %v", err)
		}

		// Validate against expected narinfo data.
		expectedNarInfo := getExpectedNarInfo(t)
		if actualNarInfo.StorePath != expectedNarInfo.StorePath {
			t.Errorf("StorePath mismatch: expected %s, got %s", expectedNarInfo.StorePath, actualNarInfo.StorePath)
		}
		if actualNarInfo.URL != expectedNarInfo.URL {
			t.Errorf("URL mismatch: expected %s, got %s", expectedNarInfo.URL, actualNarInfo.URL)
		}
		if actualNarInfo.FileHash.String() != expectedNarInfo.FileHash.String() {
			t.Errorf("FileHash mismatch: expected %s, got %s", expectedNarInfo.FileHash.String(), actualNarInfo.FileHash.String())
		}

		// Verify signature is present and valid.
		foundTestKeySignature := false
		for _, sig := range actualNarInfo.Signatures {
			if sig.Name == "depot-test-1" {
				foundTestKeySignature = true
				publicKey, err := signature.ParsePublicKey(testPublicKey)
				if err != nil {
					t.Fatalf("failed to parse test public key: %v", err)
				}
				if !publicKey.Verify(actualNarInfo.Fingerprint(), sig) {
					t.Errorf("signature verification failed for depot-test-1 signature")
				}
				break
			}
		}
		if !foundTestKeySignature {
			t.Errorf("expected narinfo to contain signature from depot-test-1")
		}

		// Verify NAR file content and hash.
		expectedNARHash := fmt.Sprintf("%x", expectedNarInfo.FileHash.Digest())
		if err := server.getSHA256(actualNarInfo.URL, expectedNARHash); err != nil {
			t.Fatalf("failed to verify NAR file hash: %v", err)
		}
	})

	t.Run("derivations and build dependencies can be pushed", func(t *testing.T) {
		// Copy sl derivation to depot.
		if err := CopyTo(server.nixLogs, server.nixLogs, ".", depotURL, true, drvPath); err != nil {
			t.Fatalf("failed to copy derivation to depot: %v", err)
		}

		// Verify the derivation's narinfo is accessible.
		drvBasename := filepath.Base(drvPath)
		drvHash := drvBasename[:32]
		drvNarInfo, err := server.getNarInfo(drvHash)
		if err != nil {
			t.Fatalf("failed to access derivation narinfo: %v", err)
		}
		if drvNarInfo.StorePath != drvPath {
			t.Errorf("Derivation StorePath mismatch: expected %s, got %s", drvPath, drvNarInfo.StorePath)
		}

		// Verify the derivation NAR file matches local content.
		narURL := fmt.Sprintf("%s/%s", depotURL, drvNarInfo.URL)
		narResp, err := http.Get(narURL)
		if err != nil {
			t.Fatalf("failed to access derivation NAR at %s: %v", narURL, err)
		}
		defer narResp.Body.Close()

		if narResp.StatusCode != http.StatusOK {
			t.Fatalf("derivation NAR not found, status: %d", narResp.StatusCode)
		}

		// Read NAR data and extract derivation content.
		remoteDrvDataXZReader, err := xz.NewReader(narResp.Body)
		if err != nil {
			t.Fatalf("failed to create XZ reader for remote derivation NAR data: %v", err)
		}
		remoteDrvDataNARReader, err := nar.NewReader(remoteDrvDataXZReader)
		if err != nil {
			t.Fatalf("failed to create NAR reader for remote derivation data: %v", err)
		}

		var remoteDrvData []byte
		for {
			hdr, err := remoteDrvDataNARReader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("failed to read NAR entry: %v", err)
			}
			if hdr.Type == nar.TypeRegular {
				remoteDrvData, err = io.ReadAll(remoteDrvDataNARReader)
				if err != nil {
					t.Fatalf("failed to read derivation content from NAR: %v", err)
				}
				break
			}
		}
		if len(remoteDrvData) == 0 {
			t.Fatalf("no derivation content found in NAR archive")
		}

		// Compare with local derivation file.
		localDrvData, err := os.ReadFile(drvPath)
		if err != nil {
			t.Fatalf("failed to read local derivation file %s: %v", drvPath, err)
		}

		actualHash := fmt.Sprintf("%x", sha256.Sum256(remoteDrvData))
		expectedHash := fmt.Sprintf("%x", sha256.Sum256(localDrvData))
		if expectedHash != actualHash {
			t.Errorf("local and remote derivation files do not match")
		}

		// Copy the inputs to the sl derivation, and any input sources.
		inputDerivations, inputSrcs, err := DerivationShow(server.nixLogs, server.nixLogs, ".", slPath)
		if err != nil {
			t.Fatalf("failed to get derivation info: %v", err)
		}
		if len(inputDerivations) == 0 {
			t.Fatalf("no input derivations found for package %s", slPath)
		}
		allInputs := append(inputSrcs, inputDerivations...)

		// Realise all input derivations to get their store paths.
		realisedPaths, err := RealiseStorePaths(server.nixLogs, server.nixLogs, allInputs...)
		if err != nil {
			t.Fatalf("failed to realise input derivations: %v", err)
		}

		// Copy all the realised paths to depot.
		if err := CopyTo(server.nixLogs, server.nixLogs, ".", depotURL, false, realisedPaths...); err != nil {
			t.Fatalf("failed to copy realised input derivations to depot: %v", err)
		}

		// Verify that all realised paths are accessible in depot.
		for _, path := range realisedPaths {
			pathHashPart := filepath.Base(path)[:32]
			narInfo, err := server.getNarInfo(pathHashPart)
			if err != nil {
				t.Fatalf("failed to access narinfo for hashpart %s: %v", pathHashPart, err)
			}
			if narInfo.StorePath != path {
				t.Errorf("StorePath mismatch for %s: expected %s, got %s", path, narInfo.StorePath, path)
			}
		}
	})

	t.Run("flake sources can be pushed", func(t *testing.T) {
		// Archive the flake to our depot.
		flakeRef := "github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526"
		if err := FlakeArchive(server.nixLogs, server.nixLogs, depotURL, flakeRef); err != nil {
			t.Fatalf("failed to archive flake to depot: %v", err)
		}

		// Verify that the flake source path is accessible.
		expectedFlakeHash := "mg5riyrz6hva7njw82gr5ghvajklkccq"
		flakeNarInfo, err := server.getNarInfo(expectedFlakeHash)
		if err != nil {
			t.Fatalf("failed to access flake source narinfo: %v", err)
		}
		if flakeNarInfo.StorePath != expectedFlakeStorePath {
			t.Errorf("StorePath mismatch: expected %s, got %s", expectedFlakeStorePath, flakeNarInfo.StorePath)
		}
	})

	t.Run("packages can be copied from depot to local store", func(t *testing.T) {
		// Create a temporary local store directory.
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("failed to get user home directory: %v", err)
		}
		tempStore, err := os.MkdirTemp(home, "depot-test-store-*")
		if err != nil {
			t.Fatalf("failed to create temp store: %v", err)
		}
		defer os.RemoveAll(tempStore)

		// Test copying from the depot to a local store.
		if err := CopyFrom(server.nixLogs, server.nixLogs, tempStore, depotURL, slPath, expectedFlakeStorePath); err != nil {
			t.Fatalf("failed to copy from depot to local store: %v", err)
		}

		// Get narinfo for dependencies.
		actualNarInfo, err := server.getNarInfo(hashPart)
		if err != nil {
			t.Fatalf("failed to get narinfo for dependencies: %v", err)
		}

		// Build expectations based on actual dependencies.
		expectations := map[string]bool{
			filepath.Base(slPath):                 false,
			filepath.Base(expectedFlakeStorePath): false,
		}
		for _, ref := range actualNarInfo.References {
			expectations[ref] = false
		}

		// Verify all expected paths were copied.
		err = filepath.Walk(tempStore, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			basePath := filepath.Base(p)
			if _, ok := expectations[basePath]; ok {
				expectations[basePath] = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("error walking temp store: %v", err)
		}

		for name, found := range expectations {
			if !found {
				t.Errorf("expected path %s not found in local store copy", name)
			}
		}
	})

	t.Run("cache info includes public key", func(t *testing.T) {
		// Verify nix-cache-info includes the public key.
		resp, err := server.get("nix-cache-info")
		if err != nil {
			t.Fatalf("failed to get nix-cache-info: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("nix-cache-info returned status %d, expected 200", resp.StatusCode)
		}

		cacheInfoBody, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read nix-cache-info response: %v", err)
		}

		if !strings.Contains(string(cacheInfoBody), "PublicKey: depot-test-1:AbJWtPERDd0WHXZOqUhJH1tSwXIGO5Z92km3bqVhf/k=") {
			t.Errorf("nix-cache-info should contain our test public key")
		}
	})
}
