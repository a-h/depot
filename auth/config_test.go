package auth_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-h/depot/auth"
	"golang.org/x/crypto/ssh"
)

func TestAuthFilesAreParsed(t *testing.T) {
	for _, keyType := range keyTypes {
		t.Run(keyType+" keys are loaded from the auth file", func(t *testing.T) {
			writer, err := newSigner(keyType)
			if err != nil {
				t.Fatalf("failed to create signer: %v", err)
			}
			reader, err := newSigner(keyType)
			if err != nil {
				t.Fatalf("failed to create signer: %v", err)
			}

			contents := fmt.Sprintf("# A comment.\n\nw %s deploy@ci\nr %s readonly@desktop\n",
				strings.TrimSpace(string(ssh.MarshalAuthorizedKey(writer.PublicKey()))),
				strings.TrimSpace(string(ssh.MarshalAuthorizedKey(reader.PublicKey()))))

			path := filepath.Join(t.TempDir(), "auth.keys")
			if err = os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatalf("failed to write auth file: %v", err)
			}

			config, err := auth.LoadAuthConfig(path)
			if err != nil {
				t.Fatalf("failed to load auth config: %v", err)
			}
			if len(config.Keys) != 2 {
				t.Fatalf("expected 2 keys, got %d", len(config.Keys))
			}
			if config.Keys[0].Comment != "deploy@ci" {
				t.Errorf("expected comment %q, got %q", "deploy@ci", config.Keys[0].Comment)
			}
			if !config.HasWritePermission(writer.PublicKey()) {
				t.Error("expected the write key to have write permission")
			}
			if config.HasWritePermission(reader.PublicKey()) {
				t.Error("expected the read key to not have write permission")
			}
			if !config.HasReadPermission(reader.PublicKey()) {
				t.Error("expected the read key to have read permission")
			}
		})
	}
}

func TestAuthFilesWithAReadKeyRequireAuthenticationForReads(t *testing.T) {
	tests := []struct {
		name        string
		permissions []auth.Permission
		expected    bool
	}{
		{name: "write only keys do not require authentication for reads", permissions: []auth.Permission{auth.PermissionReadWrite}, expected: false},
		{name: "a read key requires authentication for reads", permissions: []auth.Permission{auth.PermissionReadWrite, auth.PermissionRead}, expected: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var contents strings.Builder
			for _, permission := range test.permissions {
				signer, err := newSigner("ed25519")
				if err != nil {
					t.Fatalf("failed to create signer: %v", err)
				}
				fmt.Fprintf(&contents, "%s %s test\n", permission, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))))
			}

			path := filepath.Join(t.TempDir(), "auth.keys")
			if err := os.WriteFile(path, []byte(contents.String()), 0600); err != nil {
				t.Fatalf("failed to write auth file: %v", err)
			}

			config, err := auth.LoadAuthConfig(path)
			if err != nil {
				t.Fatalf("failed to load auth config: %v", err)
			}
			if config.RequireAuthForRead != test.expected {
				t.Errorf("expected RequireAuthForRead to be %v, got %v", test.expected, config.RequireAuthForRead)
			}

			unknown, err := newSigner("ed25519")
			if err != nil {
				t.Fatalf("failed to create signer: %v", err)
			}
			if got := config.HasReadPermission(unknown.PublicKey()); got == test.expected {
				t.Errorf("expected unknown keys to have read permission %v, got %v", !test.expected, got)
			}
		})
	}
}

func TestAuthFilesAreValidated(t *testing.T) {
	tests := []struct {
		name     string
		contents string
	}{
		{name: "an invalid permission is rejected", contents: "x ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIB test"},
		{name: "a line with too few fields is rejected", contents: "w ssh-ed25519"},
		{name: "an invalid key is rejected", contents: "w ssh-ed25519 not-base64 test"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.keys")
			if err := os.WriteFile(path, []byte(test.contents), 0600); err != nil {
				t.Fatalf("failed to write auth file: %v", err)
			}

			if _, err := auth.LoadAuthConfig(path); err == nil {
				t.Fatal("expected loading to fail, but it succeeded")
			}
		})
	}

	t.Run("a missing auth file is rejected", func(t *testing.T) {
		if _, err := auth.LoadAuthConfig(filepath.Join(t.TempDir(), "missing.keys")); err == nil {
			t.Fatal("expected loading to fail, but it succeeded")
		}
	})

	t.Run("an empty path loads no keys", func(t *testing.T) {
		config, err := auth.LoadAuthConfig("")
		if err != nil {
			t.Fatalf("failed to load auth config: %v", err)
		}
		if len(config.Keys) != 0 {
			t.Errorf("expected 0 keys, got %d", len(config.Keys))
		}
	})
}
