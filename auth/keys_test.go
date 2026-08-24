package auth_test

import (
	"crypto"
	"encoding/pem"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-h/depot/auth"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// sshAgent is an SSH agent listening on a unix socket, holding a single key.
type sshAgent struct {
	socket   string
	dir      string
	listener net.Listener
}

// startAgentWithKey starts an SSH agent holding the given key.
func startAgentWithKey(key crypto.Signer) (*sshAgent, error) {
	dir, err := os.MkdirTemp("", "depot-agent")
	if err != nil {
		return nil, err
	}

	a := &sshAgent{socket: filepath.Join(dir, "agent.sock"), dir: dir}
	if a.listener, err = net.Listen("unix", a.socket); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}

	keyring := agent.NewKeyring()
	if err = keyring.Add(agent.AddedKey{PrivateKey: key, Comment: "test"}); err != nil {
		a.Close()
		return nil, err
	}
	go a.serve(keyring)

	return a, nil
}

func (a *sshAgent) serve(keyring agent.Agent) {
	for {
		conn, err := a.listener.Accept()
		if err != nil {
			return
		}
		go agent.ServeAgent(keyring, conn)
	}
}

func (a *sshAgent) Close() error {
	err := a.listener.Close()
	if removeErr := os.RemoveAll(a.dir); err == nil {
		err = removeErr
	}
	return err
}

// writeKeyFiles writes the requested halves of an OpenSSH key pair to dir.
func writeKeyFiles(dir, name string, key crypto.Signer, private, public bool) (ssh.PublicKey, error) {
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, err
	}
	if public {
		if err = os.WriteFile(filepath.Join(dir, name+".pub"), ssh.MarshalAuthorizedKey(signer.PublicKey()), 0644); err != nil {
			return nil, err
		}
	}
	if private {
		block, err := ssh.MarshalPrivateKey(key, "test")
		if err != nil {
			return nil, err
		}
		if err = os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0600); err != nil {
			return nil, err
		}
	}
	return signer.PublicKey(), nil
}

func TestSSHAgentKeysCreateTokensThatVerify(t *testing.T) {
	for _, keyType := range keyTypes {
		t.Run(keyType+" keys held by ssh-agent create tokens that verify", func(t *testing.T) {
			key, err := newPrivateKey(keyType)
			if err != nil {
				t.Fatalf("failed to generate key: %v", err)
			}
			signer, err := ssh.NewSignerFromKey(key)
			if err != nil {
				t.Fatalf("failed to create signer: %v", err)
			}

			a, err := startAgentWithKey(key)
			if err != nil {
				t.Fatalf("failed to start agent: %v", err)
			}
			defer a.Close()

			t.Setenv("SSH_AUTH_SOCK", a.socket)
			t.Setenv("HOME", t.TempDir())

			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			keys, err := auth.DiscoverSSHKeys(log)
			if err != nil {
				t.Fatalf("failed to discover keys: %v", err)
			}
			if len(keys) != 1 {
				t.Fatalf("expected 1 key, got %d", len(keys))
			}
			if keys[0].Source != "agent" {
				t.Fatalf("expected key from agent, got %q", keys[0].Source)
			}

			token, err := auth.CreateJWT(keys[0].Signer)
			if err != nil {
				t.Fatalf("failed to create token: %v", err)
			}

			config := &auth.AuthConfig{
				Keys: []auth.AuthorizedKey{{Permission: auth.PermissionReadWrite, PublicKey: signer.PublicKey()}},
			}
			fingerprint, err := auth.VerifyJWT(token, config)
			if err != nil {
				t.Fatalf("failed to verify token: %v", err)
			}
			if expected := ssh.FingerprintSHA256(signer.PublicKey()); fingerprint != expected {
				t.Errorf("expected fingerprint %q, got %q", expected, fingerprint)
			}
		})
	}
}

func TestIdentitiesSelectAKey(t *testing.T) {
	tests := []struct {
		name string
		// dir is the directory within the home directory that the key is written to.
		dir string
		// writePrivate and writePublic control which key files exist.
		writePrivate bool
		writePublic  bool
		// inAgent holds the key in an SSH agent for the duration of the test.
		inAgent bool
		// identity is the path passed to LoadIdentity, relative to the home directory unless it starts with ~.
		identity string
		// selectsKey is true if LoadIdentity is expected to return the written key.
		selectsKey bool
	}{
		{
			name:         "a private key file is used",
			writePrivate: true,
			writePublic:  true,
			identity:     "id_ed25519",
			selectsKey:   true,
		},
		{
			name:        "a public key file selects the matching agent key",
			writePublic: true,
			inAgent:     true,
			identity:    "id_ed25519.pub",
			selectsKey:  true,
		},
		{
			name:        "a private key path falls back to the agent when the private key is unavailable",
			writePublic: true,
			inAgent:     true,
			identity:    "id_ed25519",
			selectsKey:  true,
		},
		{
			name:         "a path starting with a tilde is expanded",
			dir:          ".ssh",
			writePrivate: true,
			writePublic:  true,
			identity:     "~/.ssh/id_ed25519",
			selectsKey:   true,
		},
		{
			name:     "an identity that does not exist is rejected",
			identity: "id_missing",
		},
		{
			name:        "an identity with no private key and no agent key is rejected",
			writePublic: true,
			identity:    "id_ed25519.pub",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			dir := filepath.Join(home, test.dir)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatalf("failed to create key dir: %v", err)
			}

			key, err := newPrivateKey("ed25519")
			if err != nil {
				t.Fatalf("failed to generate key: %v", err)
			}
			publicKey, err := writeKeyFiles(dir, "id_ed25519", key, test.writePrivate, test.writePublic)
			if err != nil {
				t.Fatalf("failed to write key files: %v", err)
			}

			// Point at a socket that does not exist, so that a real ssh-agent or gpg-agent is not used.
			socket := filepath.Join(home, "none.sock")
			if test.inAgent {
				a, err := startAgentWithKey(key)
				if err != nil {
					t.Fatalf("failed to start agent: %v", err)
				}
				defer a.Close()
				socket = a.socket
			}
			t.Setenv("SSH_AUTH_SOCK", socket)

			identity := test.identity
			if !strings.HasPrefix(identity, "~/") {
				identity = filepath.Join(dir, identity)
			}

			signer, err := auth.LoadIdentity(slog.New(slog.NewTextHandler(io.Discard, nil)), identity)
			if !test.selectsKey {
				if err == nil {
					t.Fatal("expected loading to fail, but it succeeded")
				}
				return
			}
			if err != nil {
				t.Fatalf("failed to load identity: %v", err)
			}
			if got, want := ssh.FingerprintSHA256(signer.PublicKey()), ssh.FingerprintSHA256(publicKey); got != want {
				t.Errorf("expected fingerprint %q, got %q", want, got)
			}
		})
	}
}
