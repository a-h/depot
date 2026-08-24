package auth

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
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
func DiscoverSSHKeys(log *slog.Logger) (out []KeyInfo, err error) {
	log.Debug("Discovering SSH keys")
	// 1) Try ssh-agent via SSH_AUTH_SOCK, else try gpg-agent's ssh socket.
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		log.Debug("SSH_AUTH_SOCK not set, trying gpg-agent's SSH socket")
		s, err := gpgAgentSSHSock()
		if err != nil {
			log.Debug("error getting gpg-agent SSH socket", slog.Any("error", err))
		}
		if err == nil && s != "" {
			sock = s
			log.Debug("using gpg-agent SSH socket", slog.String("socket", sock))
		}
	}
	if sock != "" {
		log.Debug("listing agent keys", slog.String("socket", sock))
		kis, err := listAgentKeys(sock)
		if err != nil {
			log.Warn("failed to list SSH agent keys", slog.Any("error", err))
		}
		if err == nil {
			out = append(out, kis...)
		}
	}

	// 2) Fallback: scan ~/.ssh/*.pub files.
	log.Debug("Scanning ~/.ssh directory for key files")
	kis, err := listFileKeys()
	if err != nil {
		log.Warn("failed to scan for key files", slog.Any("error", err))
	}
	if err == nil {
		out = append(out, kis...)
	}

	return out, nil
}

// LoadIdentity loads the key at path, falling back to a matching key from an agent if the private key
// cannot be read, in the same way as ssh -i.
func LoadIdentity(log *slog.Logger, path string) (ssh.Signer, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user home directory: %w", err)
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}

	publicKeyPath := path
	if !strings.HasSuffix(path, ".pub") {
		signer, err := loadPrivateKey(path)
		if err == nil {
			log.Debug("loaded identity from private key file", slog.String("path", path))
			return signer, nil
		}
		log.Debug("failed to load private key, looking for a matching key in the agent", slog.String("path", path), slog.Any("error", err))
		publicKeyPath = path + ".pub"
	}

	data, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read identity %s: %w", path, err)
	}
	publicKey, _, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse identity %s: %w", path, err)
	}

	fingerprint := ssh.FingerprintSHA256(publicKey)
	keys, err := DiscoverSSHKeys(log)
	if err != nil {
		return nil, fmt.Errorf("failed to discover SSH keys: %w", err)
	}
	for _, keyInfo := range keys {
		if keyInfo.Fingerprint == fingerprint && keyInfo.Signer != nil {
			log.Debug("loaded identity", slog.String("path", publicKeyPath), slog.String("source", keyInfo.Source))
			return keyInfo.Signer, nil
		}
	}

	return nil, fmt.Errorf("no private key or agent key found for identity %s", path)
}

// listAgentKeys lists SSH keys from ssh-agent.
func listAgentKeys(sock string) (out []KeyInfo, err error) {
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
			socket:    sock,
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

// agentSigner implements ssh.AlgorithmSigner using ssh-agent, so that RSA keys can be signed with
// SHA-256 instead of the SSH default of SHA-1.
type agentSigner struct {
	socket    string
	publicKey ssh.PublicKey
}

var _ ssh.AlgorithmSigner = (*agentSigner)(nil)

func (s *agentSigner) PublicKey() ssh.PublicKey {
	return s.publicKey
}

func (s *agentSigner) Sign(rand io.Reader, data []byte) (*ssh.Signature, error) {
	return s.SignWithAlgorithm(rand, data, s.publicKey.Type())
}

func (s *agentSigner) SignWithAlgorithm(rand io.Reader, data []byte, algorithm string) (*ssh.Signature, error) {
	var flags agent.SignatureFlags
	switch algorithm {
	case "", s.publicKey.Type():
	case ssh.KeyAlgoRSASHA256:
		flags = agent.SignatureFlagRsaSha256
	case ssh.KeyAlgoRSASHA512:
		flags = agent.SignatureFlagRsaSha512
	default:
		return nil, fmt.Errorf("unsupported signature algorithm: %s", algorithm)
	}

	// Reconnect to agent for each signature to avoid connection lifecycle issues.
	conn, err := net.Dial("unix", s.socket)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ssh-agent: %w", err)
	}
	defer conn.Close()

	return agent.NewClient(conn).SignWithFlags(s.publicKey, data, flags)
}
