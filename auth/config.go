package auth

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Permission represents the type of access a key has.
type Permission string

const (
	PermissionRead      Permission = "r"
	PermissionReadWrite Permission = "w"
)

// AuthConfig holds the SSH public keys and their permissions.
type AuthConfig struct {
	Keys               []AuthorizedKey
	RequireAuthForRead bool // True if any key has read-only permission
}

// AuthorizedKey represents an SSH public key with its permission level.
type AuthorizedKey struct {
	Permission Permission
	PublicKey  ssh.PublicKey
	Comment    string
}

// LoadAuthConfig loads authentication configuration from a file.
// File format: each line contains "r/w ssh-keytype base64key comment"
func LoadAuthConfig(filepath string) (*AuthConfig, error) {
	if filepath == "" {
		return &AuthConfig{}, nil
	}

	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open auth file: %w", err)
	}
	defer file.Close()

	var config AuthConfig
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 3 {
			return nil, fmt.Errorf("invalid format on line %d: expected at least 3 fields", lineNum)
		}

		// Parse permission.
		var perm Permission
		switch parts[0] {
		case "r":
			perm = PermissionRead
			config.RequireAuthForRead = true
		case "w":
			perm = PermissionReadWrite
		default:
			return nil, fmt.Errorf("invalid permission on line %d: expected 'r' or 'w', got '%s'", lineNum, parts[0])
		}

		// Parse SSH public key.
		keyLine := strings.Join(parts[1:], " ")
		pubKey, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(keyLine))
		if err != nil {
			return nil, fmt.Errorf("invalid SSH key on line %d: %w", lineNum, err)
		}

		config.Keys = append(config.Keys, AuthorizedKey{
			Permission: perm,
			PublicKey:  pubKey,
			Comment:    comment,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading auth file: %w", err)
	}

	return &config, nil
}

// IsAuthorized checks if a public key is authorized and returns the permission level.
func (c *AuthConfig) IsAuthorized(pubKey ssh.PublicKey) (Permission, bool) {
	for _, key := range c.Keys {
		if string(key.PublicKey.Marshal()) == string(pubKey.Marshal()) {
			return key.Permission, true
		}
	}
	return "", false
}

// HasWritePermission checks if a public key has write permissions.
func (c *AuthConfig) HasWritePermission(pubKey ssh.PublicKey) bool {
	perm, authorized := c.IsAuthorized(pubKey)
	return authorized && perm == PermissionReadWrite
}

// HasReadPermission checks if a public key has read permissions.
func (c *AuthConfig) HasReadPermission(pubKey ssh.PublicKey) bool {
	if !c.RequireAuthForRead {
		return true
	}
	perm, authorized := c.IsAuthorized(pubKey)
	return authorized && (perm == PermissionRead || perm == PermissionReadWrite)
}
