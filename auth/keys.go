package auth

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// KeyInfo contains information about discovered SSH keys.
type KeyInfo struct {
	Source      string // "agent" or "file"
	Alg         string
	Fingerprint string // SHA256
	Comment     string
	Hints       []string   // e.g. "fido2", "gpg-agent", "yubikey?"
	Signer      ssh.Signer // The actual signer if available
}

// DiscoverSSHKeys discovers available SSH keys from ssh-agent and ~/.ssh/ directory.
func DiscoverSSHKeys() ([]KeyInfo, error) {
	var out []KeyInfo

	// 1) Try ssh-agent via SSH_AUTH_SOCK, else try gpg-agent's ssh socket.
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		if p, err := gpgAgentSSHSock(); err == nil && p != "" {
			sock = p
		}
	}
	if sock != "" {
		if kis, err := listAgentKeys(sock); err == nil {
			out = append(out, kis...)
		}
	}

	// 2) Fallback: scan ~/.ssh/*.pub files.
	if kis, err := listFileKeys(); err == nil {
		out = append(out, kis...)
	}

	return out, nil
}

// listAgentKeys lists SSH keys from ssh-agent.
func listAgentKeys(sock string) ([]KeyInfo, error) {
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	ac := agent.NewClient(conn)
	keys, err := ac.List()
	if err != nil {
		return nil, err
	}

	var out []KeyInfo
	for _, k := range keys {
		pub, err := ssh.ParsePublicKey(k.Marshal())
		if err != nil {
			continue
		}
		alg := algorithmName(pub.Type())
		fp := ssh.FingerprintSHA256(pub)
		hints := classify(pub.Type(), k.Comment)

		// Create a signer for this key.
		signer := &agentSigner{
			agent:     ac,
			publicKey: pub,
		}

		out = append(out, KeyInfo{
			Source:      "agent",
			Alg:         alg,
			Fingerprint: fp,
			Comment:     strings.TrimSpace(k.Comment),
			Hints:       hints,
			Signer:      signer,
		})
	}
	return out, nil
}

// listFileKeys lists SSH keys from ~/.ssh/*.pub files.
func listFileKeys() ([]KeyInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".ssh", "*.pub"))

	var out []KeyInfo
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		// Support one key per file (usual case).
		fields := bytes.Fields(data)
		if len(fields) < 2 {
			continue
		}
		pub, _, _, _, err := ssh.ParseAuthorizedKey(data)
		if err != nil {
			continue
		}
		alg := algorithmName(pub.Type())
		fp := ssh.FingerprintSHA256(pub)
		comment := ""
		if len(fields) >= 3 {
			comment = string(bytes.Join(fields[2:], []byte(" ")))
		}
		hints := classify(pub.Type(), comment)

		// Try to load the corresponding private key file.
		privateKeyPath := strings.TrimSuffix(p, ".pub")
		signer, err := loadPrivateKey(privateKeyPath)
		if err != nil {
			// Private key not available or encrypted.
			signer = nil
		}

		out = append(out, KeyInfo{
			Source:      "file",
			Alg:         alg,
			Fingerprint: fp,
			Comment:     strings.TrimSpace(comment),
			Hints:       hints,
			Signer:      signer,
		})
	}
	return out, nil
}

// algorithmName normalizes algorithm names.
func algorithmName(t string) string {
	switch t {
	case "ssh-ed25519":
		return "ed25519"
	case "ssh-rsa":
		return "rsa"
	case "ecdsa-sha2-nistp256":
		return "ecdsa-p256"
	case "sk-ecdsa-sha2-nistp256@openssh.com":
		return "ecdsa-sk" // FIDO2 security key
	case "sk-ssh-ed25519@openssh.com":
		return "ed25519-sk" // FIDO2 security key
	default:
		return t
	}
}

// classify provides hints about the key type.
func classify(pubType, comment string) []string {
	var hints []string
	if strings.Contains(pubType, "-sk") || strings.HasPrefix(pubType, "sk-") {
		hints = append(hints, "fido2")
	}
	// gpg-agent often appends "cardno:XXXX" to comments; keep heuristic loose.
	c := strings.ToLower(comment)
	if strings.Contains(c, "cardno:") || strings.Contains(c, "gpg") {
		hints = append(hints, "gpg-agent")
	}
	// Some setups add "YubiKey" in comment or are FIDO2-backed (common on YubiKey).
	if strings.Contains(c, "yubikey") {
		hints = append(hints, "yubikey?")
	}
	return hints
}

// gpgAgentSSHSock gets the GPG agent SSH socket path.
func gpgAgentSSHSock() (string, error) {
	// Prefer asking gpgconf; works even when SSH_AUTH_SOCK is unset.
	cmd := exec.Command("gpgconf", "--list-dirs", "agent-ssh-socket")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// loadPrivateKey attempts to load a private key from disk.
func loadPrivateKey(path string) (ssh.Signer, error) {
	keyData, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Try to parse without passphrase first.
	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		// Key might be encrypted - for now, we don't handle passphrases.
		return nil, fmt.Errorf("encrypted keys not supported: %w", err)
	}

	return signer, nil
}

// agentSigner implements ssh.Signer using ssh-agent.
type agentSigner struct {
	agent     agent.Agent
	publicKey ssh.PublicKey
}

func (s *agentSigner) PublicKey() ssh.PublicKey {
	return s.publicKey
}

func (s *agentSigner) Sign(rand io.Reader, data []byte) (*ssh.Signature, error) {
	return s.agent.Sign(s.publicKey, data)
}
