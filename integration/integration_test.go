package integration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/a-h/depot/db"
	"github.com/a-h/depot/handlers"
	"github.com/nix-community/go-nix/pkg/nar"
	"github.com/nix-community/go-nix/pkg/narinfo"
	"github.com/nix-community/go-nix/pkg/narinfo/signature"
	"github.com/nix-community/go-nix/pkg/sqlite/binary_cache_v6"
	"github.com/ulikunitz/xz"
)

const (
	depotURL = "http://localhost:8080"
	testPkg  = "github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl"

	// Test signing keys - these are test keys only, safe to commit to repo.
	testPrivateKey = "depot-test-1:I9FcLfz77TAEhqkIbQvPq3ecVn8A4Eml8SBek3Vk6TgBsla08REN3RYddk6pSEkfW1LBcgY7ln3aSbdupWF/+Q=="
	testPublicKey  = "depot-test-1:AbJWtPERDd0WHXZOqUhJH1tSwXIGO5Z92km3bqVhf/k="
)

type testServer struct {
	server  *http.Server
	tempDir string
	done    chan struct{}
	started chan struct{}
	sqlDB   *sql.DB
	cacheDB *binary_cache_v6.Queries
}

func (ts *testServer) start(t *testing.T) {
	var err error
	ts.tempDir, err = os.MkdirTemp("", "depot-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	ts.done = make(chan struct{})
	ts.started = make(chan struct{})

	storePath := filepath.Join(ts.tempDir, "store")
	cacheURL := depotURL

	// Initialize database and handlers.
	sqlDB, cacheDB, err := db.Init(storePath, cacheURL)
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}

	// Store database references for verification.
	ts.sqlDB = sqlDB
	ts.cacheDB = cacheDB

	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
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
		Handler: handlers.New(log, cacheDB, storePath, nil, &privateKey),
	}

	// Start server in goroutine.
	go func() {
		defer ts.sqlDB.Close()
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

func (ts *testServer) stop() {
	if ts.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ts.server.Shutdown(ctx)
		<-ts.done
	}
	if ts.tempDir != "" {
		os.RemoveAll(ts.tempDir)
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

func TestUploadPackageFromPublicCache(t *testing.T) {
	server := &testServer{}
	server.start(t)
	defer server.stop()

	// Get the store path for sl package.
	// nix eval github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl --raw
	slPath, err := Eval(os.Stdout, os.Stderr, testPkg)
	if err != nil {
		t.Fatalf("failed to evaluate sl package: %v", err)
	}

	// /nix/store/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05
	t.Logf("Store path for sl: %s", slPath)

	// Copy the package to depot.
	// nix copy --refresh --to http://localhost:8080 /nix/store/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05
	t.Logf("Copying package %s to depot %s", slPath, depotURL)
	if err := CopyTo(os.Stdout, os.Stderr, ".", depotURL, false, slPath); err != nil {
		t.Fatalf("failed to copy sl package to depot: %v", err)
	}

	// If it's present, expect the following:
	// /nix/store/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05
	// {"time":"2025-09-15T17:19:29.479083+01:00","level":"INFO","msg":"GET /4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg.narinfo","status":200,"bytes":428,"ms":0}
	// /nix/store/m7ys2iqah82aa0409qmgsnas4y0p53ci-ncurses-6.5
	// {"time":"2025-09-15T17:19:29.480411+01:00","level":"INFO","msg":"GET /m7ys2iqah82aa0409qmgsnas4y0p53ci.narinfo","status":200,"bytes":440,"ms":0}

	// Extract hash from store path for verification.
	// Store path format: /nix/store/HASH-name
	if len(slPath) < 32+len("/nix/store/") {
		t.Fatalf("store path too short to contain valid hash: %s", slPath)
	}
	hashPart := filepath.Base(slPath)[:32]

	// Verify the package's narinfo is accessible and contains correct data.
	actualNarInfo, err := server.getNarInfo(hashPart)
	if err != nil {
		t.Fatalf("failed to parse narinfo: %v", err)
	}

	// Validate the parsed narinfo contains expected data.
	// Retrieved by:
	// curl https://cache.nixos.org/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg.narinfo
	expectedNarInfo, err := narinfo.Parse(strings.NewReader(`StorePath: /nix/store/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05
URL: nar/1cq669mqpzdm7c3r411mj5452v8fn26f4p2knnxfa7rqccjh5a5f.nar.xz
Compression: xz
FileHash: sha256:1cq669mqpzdm7c3r411mj5452v8fn26f4p2knnxfa7rqccjh5a5f
FileSize: 7292
NarHash: sha256:01k25dsan4vya77pzr1wc7qhml3fqsgiqll29mv42va6l3a59q4m
NarSize: 54632
References: m7ys2iqah82aa0409qmgsnas4y0p53ci-ncurses-6.5
Deriver: 5kl200crr6r3hxmpwhcxxh8ql3f30v29-sl-5.05.drv`))
	if err != nil {
		t.Fatalf("failed to parse expected narinfo: %v", err)
	}

	if actualNarInfo.StorePath != expectedNarInfo.StorePath {
		t.Errorf("StorePath mismatch: expected %s, got %s", expectedNarInfo.StorePath, actualNarInfo.StorePath)
	}
	if actualNarInfo.URL != expectedNarInfo.URL {
		t.Errorf("URL mismatch: expected %s, got %s", expectedNarInfo.URL, actualNarInfo.URL)
	}
	if actualNarInfo.Compression != expectedNarInfo.Compression {
		t.Errorf("Compression mismatch: expected %s, got %s", expectedNarInfo.Compression, actualNarInfo.Compression)
	}
	if actualNarInfo.FileHash.String() != expectedNarInfo.FileHash.String() {
		t.Errorf("FileHash mismatch: expected %s, got %s", expectedNarInfo.FileHash.String(), actualNarInfo.FileHash.String())
	}
	if actualNarInfo.FileSize != expectedNarInfo.FileSize {
		t.Errorf("FileSize mismatch: expected %d, got %d", expectedNarInfo.FileSize, actualNarInfo.FileSize)
	}
	if actualNarInfo.NarHash.String() != expectedNarInfo.NarHash.String() {
		t.Errorf("NarHash mismatch: expected %s, got %s", expectedNarInfo.NarHash.String(), actualNarInfo.NarHash.String())
	}
	if actualNarInfo.NarSize != expectedNarInfo.NarSize {
		t.Errorf("NarSize mismatch: expected %d, got %d", expectedNarInfo.NarSize, actualNarInfo.NarSize)
	}
	if !slices.Equal(actualNarInfo.References, expectedNarInfo.References) {
		t.Errorf("References mismatch: expected %v, got %v", expectedNarInfo.References, actualNarInfo.References)
	}
	if actualNarInfo.Deriver != expectedNarInfo.Deriver {
		t.Errorf("Deriver mismatch: expected %s, got %s", expectedNarInfo.Deriver, actualNarInfo.Deriver)
	}

	// Verify signature is present and valid.
	if len(actualNarInfo.Signatures) == 0 {
		t.Errorf("expected narinfo to contain signatures, but found none")
	}
	// Verify at least one signature is from our test key.
	foundTestKeySignature := false
	for _, sig := range actualNarInfo.Signatures {
		if sig.Name == "depot-test-1" {
			foundTestKeySignature = true

			// Verify the signature is valid.
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
		t.Errorf("expected narinfo to contain signature from depot-test-1, but found signatures: %v", actualNarInfo.Signatures)
	}

	// Get the NAR file content.
	// curl -o sl.nar.xz https://cache.nixos.org/nar/1cq669mqpzdm7c3r411mj5452v8fn26f4p2knnxfa7rqccjh5a5f.nar.xz
	// sha256sum sl.nar.xz
	expectedNARHash := "aea8022563381fe5bab5535ce28cb00e6d514891350492073bb5fd8b6b3206b3"
	err = server.getSHA256(actualNarInfo.URL, expectedNARHash)
	if err != nil {
		t.Fatalf("failed to verify NAR file hash: %v", err)
	}

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
		t.Errorf("nix-cache-info should contain our test public key, got: %s", string(cacheInfoBody))
	}
}

func TestCopyDerivation(t *testing.T) {
	server := &testServer{}
	server.start(t)
	defer server.stop()

	// It's not enough to simply copy the binary package - we need the derivation, or we can't
	// run `nix run nixpkgs#sl` on the airgapped side.

	// Get the derivation path for sl package.
	// nix eval github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl.drvPath --raw
	drvPath, err := Eval(os.Stdout, os.Stderr, testPkg+".drvPath")
	if err != nil {
		t.Fatalf("failed to evaluate sl derivation: %v", err)
	}

	// /nix/store/5kl200crr6r3hxmpwhcxxh8ql3f30v29-sl-5.05.drv
	t.Logf("Derivation path for sl: %s", drvPath)

	// Copy the derivation to our depot.
	// nix copy --derivation --refresh --to http://localhost:8080 /nix/store/5kl200crr6r3hxmpwhcxxh8ql3f30v29-sl-5.05.drv
	if err := CopyTo(os.Stdout, os.Stderr, ".", depotURL, true, drvPath); err != nil {
		t.Fatalf("failed to copy derivation to depot: %v", err)
	}

	// Extract hash from derivation path for verification.
	// Derivation path format: /nix/store/HASH-name.drv
	drvBasename := filepath.Base(drvPath)
	drvHash := drvBasename[:32]

	// Verify the derivation's narinfo is accessible.
	actualNarInfo, err := server.getNarInfo(drvHash)
	if err != nil {
		t.Fatalf("failed to access derivation narinfo: %v", err)
	}
	if actualNarInfo.StorePath != drvPath {
		t.Errorf("Derivation StorePath mismatch: expected %s, got %s", drvPath, actualNarInfo.StorePath)
	}

	// Verify the derivation NAR file is accessible.
	narURL := fmt.Sprintf("%s/%s", depotURL, actualNarInfo.URL)
	narResp, err := http.Get(narURL)
	if err != nil {
		t.Fatalf("failed to access derivation NAR at %s: %v", narURL, err)
	}
	defer narResp.Body.Close()

	if narResp.StatusCode != http.StatusOK {
		t.Fatalf("derivation NAR not found, status: %d", narResp.StatusCode)
	}

	// Read NAR data.
	// It will be xz and NAR compressed.
	remoteDrvDataXZReader, err := xz.NewReader(narResp.Body)
	if err != nil {
		t.Fatalf("failed to create XZ reader for remote derivation NAR data: %v", err)
	}
	remoteDrvDataNARReader, err := nar.NewReader(remoteDrvDataXZReader)
	if err != nil {
		t.Fatalf("failed to create NAR reader for remote derivation data: %v", err)
	}

	// NAR files contain entries - we need to find the derivation file entry.
	var remoteDrvData []byte
	for {
		hdr, err := remoteDrvDataNARReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("failed to read NAR entry: %v", err)
		}

		// The derivation file should be the main entry (not a directory), and there's only one file, the drv itself.
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
	actualHash := fmt.Sprintf("%x", sha256.Sum256(remoteDrvData))

	// Read the local derivation file.
	// nix eval github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl.drvPath --raw
	// /nix/store/5kl200crr6r3hxmpwhcxxh8ql3f30v29-sl-5.05.drv
	// Hash it.
	// sha256sum /nix/store/5kl200crr6r3hxmpwhcxxh8ql3f30v29-sl-5.05.drv
	expectedHash := "ad0e8ebce4c8545b0b5643cadb790222f659d1607cea96ec24a7db5c5573e3db"

	// Compare the local derivation file with the one from the depot.
	if expectedHash != actualHash {
		t.Errorf("local (%q) and remote (%q) derivation files do not match", drvPath, narURL)
	}
}

func TestFlakeArchive(t *testing.T) {
	server := &testServer{}
	server.start(t)
	defer server.stop()

	// For remote systems to be able to build from a flake, the flake source also needs to be
	// available in the binary cache.

	// Archive the flake to our depot.
	flakeRef := "github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526"
	if err := FlakeArchive(os.Stdout, os.Stderr, depotURL, flakeRef); err != nil {
		t.Fatalf("failed to archive flake to depot: %v", err)
	}

	// Verify that the flake source path is accessible via the narinfo endpoint.
	// We can see the store path with:
	// nix flake archive --json github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526 --json | jq -r .path
	// /nix/store/mg5riyrz6hva7njw82gr5ghvajklkccq-source
	expectedHash := "mg5riyrz6hva7njw82gr5ghvajklkccq"
	actualNarInfo, err := server.getNarInfo(expectedHash)
	if err != nil {
		t.Fatalf("failed to access flake source narinfo: %v", err)
	}
	if actualNarInfo.StorePath != "/nix/store/mg5riyrz6hva7njw82gr5ghvajklkccq-source" {
		t.Errorf("StorePath mismatch: expected /nix/store/mg5riyrz6hva7njw82gr5ghvajklkccq-source, got %s", actualNarInfo.StorePath)
	}

	// curl https://cache.nixos.org/mg5riyrz6hva7njw82gr5ghvajklkccq.narinfo
	expectedNarInfo, err := narinfo.Parse(strings.NewReader(`StorePath: /nix/store/mg5riyrz6hva7njw82gr5ghvajklkccq-source
URL: nar/10mzlawkwj63dmnrsmxvj054icwqd23ma5si3rzgghw0dsdzq8sz.nar.xz
Compression: xz
FileHash: sha256:10mzlawkwj63dmnrsmxvj054icwqd23ma5si3rzgghw0dsdzq8sz
FileSize: 30528060
NarHash: sha256:0555pg9zcr3aazyxqb6g6q8vq3lc5zz3rnqx8ga1i3bs2q04yb4q
NarSize: 185066208
Sig: cache.nixos.org-1:nFS+NxdPcM46jWHq94n2CTx/0GYE9lBBtShoH8wEH1uq5RWPoLyq9t6UWzMxXHhsIfOCPdB1SUaVthPfyxpkCQ==
CA: fixed:r:sha256:0555pg9zcr3aazyxqb6g6q8vq3lc5zz3rnqx8ga1i3bs2q04yb4q`))
	if err != nil {
		t.Fatalf("failed to parse expected narinfo: %v", err)
	}

	if actualNarInfo.StorePath != expectedNarInfo.StorePath {
		t.Errorf("StorePath mismatch: expected %s, got %s", expectedNarInfo.StorePath, actualNarInfo.StorePath)
	}
	if actualNarInfo.URL != expectedNarInfo.URL {
		t.Errorf("URL mismatch: expected %s, got %s", expectedNarInfo.URL, actualNarInfo.URL)
	}
	if actualNarInfo.Compression != expectedNarInfo.Compression {
		t.Errorf("Compression mismatch: expected %s, got %s", expectedNarInfo.Compression, actualNarInfo.Compression)
	}
	if actualNarInfo.FileHash.String() != expectedNarInfo.FileHash.String() {
		t.Errorf("FileHash mismatch: expected %s, got %s", expectedNarInfo.FileHash.String(), actualNarInfo.FileHash.String())
	}
	if actualNarInfo.FileSize != expectedNarInfo.FileSize {
		t.Errorf("FileSize mismatch: expected %d, got %d", expectedNarInfo.FileSize, actualNarInfo.FileSize)
	}
	if actualNarInfo.NarHash.String() != expectedNarInfo.NarHash.String() {
		t.Errorf("NarHash mismatch: expected %s, got %s", expectedNarInfo.NarHash.String(), actualNarInfo.NarHash.String())
	}
	if actualNarInfo.NarSize != expectedNarInfo.NarSize {
		t.Errorf("NarSize mismatch: expected %d, got %d", expectedNarInfo.NarSize, actualNarInfo.NarSize)
	}
	if !slices.Equal(actualNarInfo.References, expectedNarInfo.References) {
		t.Errorf("References mismatch: expected %v, got %v", expectedNarInfo.References, actualNarInfo.References)
	}

	// curl -o source.nar.xz https://cache.nixos.org/nar/10mzlawkwj63dmnrsmxvj054icwqd23ma5si3rzgghw0dsdzq8sz.nar.xz
	// sha256sum source.nar.xz
	expectedNARHash := "5f23fc9b6e80c3f77e1e511755876898b3480a90bb579d6d6dc3483eb9a2bf82"
	err = server.getSHA256(actualNarInfo.URL, expectedNARHash)
	if err != nil {
		t.Fatalf("failed to verify flake source NAR file hash: %v", err)
	}
}

func TestRoundTripCopy(t *testing.T) {
	server := &testServer{}
	server.start(t)
	defer server.stop()

	t.Log("Evaluating")

	// Get the store path for sl package.
	// nix eval github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl --raw
	// /nix/store/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05
	slPath, err := Eval(os.Stdout, os.Stderr, testPkg)
	if err != nil {
		t.Fatalf("failed to evaluate sl package: %v", err)
	}

	t.Log("Copying to depot")

	// Copy to depot.
	// nix copy --to http://localhost:8080 /nix/store/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05 --refresh
	if err := CopyTo(os.Stdout, os.Stderr, ".", depotURL, false, slPath); err != nil {
		t.Fatalf("failed to copy to depot: %v", err)
	}

	// Verify we can get the narinfo of the binary from depot.
	// Should be 4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg
	hashPart := filepath.Base(slPath)[:32]
	actualNarInfo, err := server.getNarInfo(hashPart)
	if err != nil {
		t.Fatalf("failed to get narinfo from depot: %v", err)
	}
	if actualNarInfo.StorePath != slPath {
		t.Errorf("StorePath mismatch: expected %s, got %s", slPath, actualNarInfo.StorePath)
	}

	// Archive the flake to ensure source is available.
	flakeRef := "github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526"
	if err := FlakeArchive(os.Stdout, os.Stderr, depotURL, flakeRef); err != nil {
		t.Fatalf("failed to archive flake to depot: %v", err)
	}
	// nix flake archive --json github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526 --json | jq -r .path
	// /nix/store/mg5riyrz6hva7njw82gr5ghvajklkccq-source
	flakeStorePath := "/nix/store/mg5riyrz6hva7njw82gr5ghvajklkccq-source"

	t.Log("Verifying in depot")

	// Create a temporary local store directory in CWD to test copying from depot to local store.
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
	// This tests that the Nix CLI can use depot as a source.
	// For a complete round-trip, we need both runtime dependencies and source.
	// nix copy --no-check-sigs --from http://localhost:8080 --to ~/temp-store /nix/store/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05 /nix/store/mg5riyrz6hva7njw82gr5ghvajklkccq-source
	if err := CopyFrom(os.Stdout, os.Stderr, tempStore, depotURL, slPath, flakeStorePath); err != nil {
		t.Fatalf("failed to copy from depot to local store: %v", err)
	}

	// We should expect runtime dependencies and source:
	// ls ~/temp-store/nix/store/
	// 4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05
	// m7ys2iqah82aa0409qmgsnas4y0p53ci-ncurses-6.5
	// mg5riyrz6hva7njw82gr5ghvajklkccq-source

	// Verify the package was copied to the local store.
	expectations := map[string]bool{
		"4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05":     false,
		"m7ys2iqah82aa0409qmgsnas4y0p53ci-ncurses-6.5": false,
		"mg5riyrz6hva7njw82gr5ghvajklkccq-source":      false,
	}
	err = filepath.Walk(tempStore, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			t.Fatalf("error accessing path %s: %v", p, err)
		}
		basePath := filepath.Base(p)
		if err != nil {
			t.Fatalf("failed to get relative path for %s: %v", p, err)
		}
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
}

func TestInputDerivations(t *testing.T) {
	server := &testServer{}
	server.start(t)
	defer server.stop()

	// Get the store path for sl package.
	// nix eval github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl --raw
	// /nix/store/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05
	slPath, err := Eval(os.Stdout, os.Stderr, testPkg)
	if err != nil {
		t.Fatalf("failed to evaluate sl package: %v", err)
	}

	// Get input derivations for the sl package.
	// nix derivation show github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl | jq -r '.[].inputDrvs | keys[]'
	// /nix/store/0vyhqmxdb6h8nfmjp1qq5a6p39dfairk-stdenv-darwin.drv
	// /nix/store/b1xjkaks3nl4xj3ik46gv2mjvhif94hg-bash-5.2p37.drv
	// /nix/store/x0ynllywd6c6258h9pfca7cv1wiv6vh0-source.drv
	// /nix/store/y1jcqq5s0yvd1mbpydy672aa9jky84xl-ncurses-6.5.drv
	inputDerivations, inputSrcs, err := DerivationShow(os.Stdout, os.Stderr, ".", slPath)
	if err != nil {
		t.Fatalf("failed to get derivation info: %v", err)
	}
	if len(inputDerivations) == 0 {
		t.Fatalf("no input derivations found for package %s", slPath)
	}
	allInputs := append(inputSrcs, inputDerivations...)

	// nix-store --realise `nix derivation show github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl | jq -r '.[].inputDrvs | keys[]'`
	// /nix/store/bq6j4f1qpdycxviy53fyh2ic39mplwhk-stdenv-darwin
	// /nix/store/0c5bc0jcpw4g8qd02y02ig8mk748xywm-bash-5.2p37-man
	// /nix/store/4glb4h059c5m2di0001ipclq03yqmzfs-bash-5.2p37-info
	// /nix/store/5c8hb299k0acbypqw6j9m4znyd6b97cz-bash-5.2p37
	// /nix/store/7jazs8847wh9ap20gvyk1afkpnqaibic-bash-5.2p37-dev
	// /nix/store/b4bpb1rjqz9j9k549p1jyx4lyq5hbvxc-bash-5.2p37-doc
	// /nix/store/pyyddwilxjwq3n7065zd6xpk8r01hqjm-source
	// /nix/store/45gqd8zj3cwmcarz599m7rjs574mbv8z-ncurses-6.5-man
	// /nix/store/m7ys2iqah82aa0409qmgsnas4y0p53ci-ncurses-6.5
	// /nix/store/z42lhil8xivaavd2n5jp6b2y8zbikf7j-ncurses-6.5-dev
	realisedPaths, err := RealiseStorePaths(os.Stdout, os.Stderr, allInputs...)
	if err != nil {
		t.Fatalf("failed to realise input derivations: %v", err)
	}

	// Ensure that we are going to copy the source code.
	slices.Sort(realisedPaths)
	if _, ok := slices.BinarySearch(realisedPaths, "/nix/store/pyyddwilxjwq3n7065zd6xpk8r01hqjm-source"); !ok {
		t.Fatalf("expected source path not found in realised paths %q with inputs: %v", realisedPaths, allInputs)
	}

	// Copy all the realised paths to depot.
	if err := CopyTo(os.Stdout, os.Stderr, ".", depotURL, false, realisedPaths...); err != nil {
		t.Fatalf("failed to copy realised input derivations to depot: %v", err)
	}

	// Verify that all realised paths are accessible in depot.
	for _, path := range realisedPaths {
		hashPart := filepath.Base(path)[:32]
		narInfo, err := server.getNarInfo(hashPart)
		if err != nil {
			t.Fatalf("failed to access narinfo for hashpart %s: %v", hashPart, err)
		}
		if narInfo.StorePath != path {
			t.Errorf("StorePath mismatch for %s: expected %s, got %s", path, narInfo.StorePath, path)
		}
	}

	// Copy the binary to depot as well.
	if err := CopyTo(os.Stdout, os.Stderr, ".", depotURL, false, slPath); err != nil {
		t.Fatalf("failed to copy sl package to depot: %v", err)
	}

	// Verify the binary is accessible.
	hashPart := filepath.Base(slPath)[:32]
	actualNarInfo, err := server.getNarInfo(hashPart)
	if err != nil {
		t.Fatalf("failed to access binary narinfo: %v", err)
	}
	if actualNarInfo.StorePath != slPath {
		t.Errorf("StorePath mismatch for binary: expected %s, got %s", slPath, actualNarInfo.StorePath)
	}
}
