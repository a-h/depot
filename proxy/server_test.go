package proxy_test

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-h/depot/auth"
	authmiddleware "github.com/a-h/depot/middleware/auth"
	"github.com/a-h/depot/proxy"
	"golang.org/x/crypto/ssh"
)

func newPrivateKey(keyType string) (crypto.Signer, error) {
	switch keyType {
	case "rsa":
		return rsa.GenerateKey(rand.Reader, 2048)
	case "ecdsa":
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case "ed25519":
		_, key, err := ed25519.GenerateKey(rand.Reader)
		return key, err
	}
	return nil, fmt.Errorf("unknown key type: %s", keyType)
}

// writeKeyPair writes an OpenSSH key pair of the given type to dir.
func writeKeyPair(dir, name, keyType string) (ssh.PublicKey, error) {
	key, err := newPrivateKey(keyType)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(key, "test")
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0600); err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(dir, name+".pub"), ssh.MarshalAuthorizedKey(signer.PublicKey()), 0644); err != nil {
		return nil, err
	}
	return signer.PublicKey(), nil
}

// put sends an upload through the proxy at addr, and returns the status code.
func put(addr string) (int, error) {
	req, err := http.NewRequest(http.MethodPut, "http://"+addr+"/nix/nar/test.nar", strings.NewReader("nar"))
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func TestProxyAuthenticatesUploadsWithSSHKeysFromTheHomeDirectory(t *testing.T) {
	for _, keyType := range []string{"rsa", "ecdsa", "ed25519"} {
		t.Run(keyType+" keys authenticate uploads", func(t *testing.T) {
			home := t.TempDir()
			sshDir := filepath.Join(home, ".ssh")
			if err := os.MkdirAll(sshDir, 0700); err != nil {
				t.Fatalf("failed to create ssh dir: %v", err)
			}
			t.Setenv("HOME", home)
			// Point at a socket that does not exist, so that a real ssh-agent or gpg-agent is not used.
			t.Setenv("SSH_AUTH_SOCK", filepath.Join(home, "agent.sock"))

			publicKey, err := writeKeyPair(sshDir, "id_"+keyType, keyType)
			if err != nil {
				t.Fatalf("failed to write key pair: %v", err)
			}

			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			config := &auth.AuthConfig{
				Keys: []auth.AuthorizedKey{{Permission: auth.PermissionReadWrite, PublicKey: publicKey}},
			}

			var uploaded bool
			upstream := httptest.NewServer(authmiddleware.New(log, config, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				uploaded = true
				w.WriteHeader(http.StatusNoContent)
			})))
			defer upstream.Close()

			addr, cleanup, err := proxy.StartProxy(log, upstream.URL, http.DefaultTransport, "")
			if err != nil {
				t.Fatalf("failed to start proxy: %v", err)
			}
			defer cleanup()

			code, err := put(addr)
			if err != nil {
				t.Fatalf("failed to send request: %v", err)
			}
			if code != http.StatusNoContent {
				t.Fatalf("expected status %d, got %d", http.StatusNoContent, code)
			}
			if !uploaded {
				t.Error("expected the upload to reach the upstream server")
			}
		})
	}
}

func TestProxyUsesTheIdentityKey(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("failed to create ssh dir: %v", err)
	}
	t.Setenv("HOME", home)
	// Point at a socket that does not exist, so that a real ssh-agent or gpg-agent is not used.
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(home, "agent.sock"))

	authorized, err := writeKeyPair(sshDir, "id_authorized", "ed25519")
	if err != nil {
		t.Fatalf("failed to write key pair: %v", err)
	}
	if _, err = writeKeyPair(sshDir, "id_other", "ed25519"); err != nil {
		t.Fatalf("failed to write key pair: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	config := &auth.AuthConfig{
		Keys: []auth.AuthorizedKey{{Permission: auth.PermissionReadWrite, PublicKey: authorized}},
	}
	upstream := httptest.NewServer(authmiddleware.New(log, config, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	defer upstream.Close()

	tests := []struct {
		name     string
		identity string
		expected int
	}{
		{name: "the authorized identity is used", identity: filepath.Join(sshDir, "id_authorized"), expected: http.StatusNoContent},
		{name: "an unauthorized identity is not substituted for another key", identity: filepath.Join(sshDir, "id_other"), expected: http.StatusUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			addr, cleanup, err := proxy.StartProxy(log, upstream.URL, http.DefaultTransport, test.identity)
			if err != nil {
				t.Fatalf("failed to start proxy: %v", err)
			}
			defer cleanup()

			code, err := put(addr)
			if err != nil {
				t.Fatalf("failed to send request: %v", err)
			}
			if code != test.expected {
				t.Errorf("expected status %d, got %d", test.expected, code)
			}
		})
	}
}

func TestProxyReportsMissingIdentities(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(home, "agent.sock"))

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, err := proxy.StartProxy(log, "http://127.0.0.1:1", http.DefaultTransport, filepath.Join(home, "missing")); err == nil {
		t.Fatal("expected starting the proxy to fail, but it succeeded")
	}
}
