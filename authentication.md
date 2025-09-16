# Authentication

The server accepts a parameter to load authentication details from disk.

The authentication details are simply a list of SSH public keys - one per line, prefixed with `r` for read-only access or `w` for read/write access.

e.g.

```txt
w ssh-rsa AAAABxxxxxx adrian@laptop
r ssh-rsa ACAABxxxxxx adrian@desktop
```

This determines who can push to the cache.

If any of the items in the list are `r`, then all access to the server is authenticated, including reading.

If no details are provided, the system defaults to read-only access.

If not, then only write access is authenticated.

The auth middleware at handlers/auth/middleware.go forces the authentication.

To authenticate inbound requests, the middleware expects an Authorization header to contain a JWT that is signed by the private key of an allowed SSH key. The server only accepts secure signature algorithms. The maximum time between iat and exp of the JWT should be a day. The JWT should be checked for validity using https://github.com/golang-jwt/jwt

## Proxy

A new `depot proxy https://<remote> --port <default:43407>` provides an open proxy to the remote, but simply adds an `Authorization` header with the bearer JWT token.

It's then possible to run commands like `nix copy --to http://localhost:43407 nixpkgs#sl` and depot will be used as a proxy.

It logs the HTTP request in a super-simple format, e.g. `GET /dsfdsf.nar` etc.

It looks for SSH keys in well known locations to use to sign the authentication JWT. I'm not sure if there's capability in https://pkg.go.dev/golang.org/x/crypto/ssh to handle this.

This code sample might help - we need to support SSH auth that might use a Yubikey or similar to store the SSH key.

```go
package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type KeyInfo struct {
	Source      string // "agent" or "file"
	Alg         string
	Fingerprint string // SHA256
	Comment     string
	Hints       []string // e.g. "fido2", "gpg-agent", "yubikey?"
}

func main() {
	var out []KeyInfo

	// 1) Try ssh-agent via SSH_AUTH_SOCK, else try gpg-agent’s ssh socket.
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

	// 2) Fallback: scan ~/.ssh/*.pub
	if kis, err := listFileKeys(); err == nil {
		out = append(out, kis...)
	}

	// Print results (compact)
	for _, k := range out {
		fmt.Printf("[%s] %s %s %s", k.Source, k.Alg, k.Fingerprint, k.Comment)
		if len(k.Hints) > 0 {
			fmt.Printf("  (%s)", strings.Join(k.Hints, ", "))
		}
		fmt.Println()
	}
}

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
		pub, err := xssh.ParsePublicKey(k.Marshal())
		if err != nil {
			continue
		}
		alg := algorithmName(pub.Type())
		fp := xssh.FingerprintSHA256(pub)
		hints := classify(pub.Type(), k.Comment)
		out = append(out, KeyInfo{
			Source:      "agent",
			Alg:         alg,
			Fingerprint: fp,
			Comment:     strings.TrimSpace(k.Comment),
			Hints:       hints,
		})
	}
	return out, nil
}

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
		// Support one key per file (usual case)
		fields := bytes.Fields(data)
		if len(fields) < 2 {
			continue
		}
		pub, err := xssh.ParseAuthorizedKey(data)
		if err != nil {
			continue
		}
		alg := algorithmName(pub.Type())
		fp := xssh.FingerprintSHA256(pub)
		comment := ""
		if len(fields) >= 3 {
			comment = string(bytes.Join(fields[2:], []byte(" ")))
		}
		hints := classify(pub.Type(), comment)
		out = append(out, KeyInfo{
			Source:      "file",
			Alg:         alg,
			Fingerprint: fp,
			Comment:     strings.TrimSpace(comment),
			Hints:       hints,
		})
	}
	return out, nil
}

func algorithmName(t string) string {
	// Normalize a bit
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

func gpgAgentSSHSock() (string, error) {
	// Prefer asking gpgconf; works even when SSH_AUTH_SOCK is unset.
	cmd := exec.Command("gpgconf", "--list-dirs", "agent-ssh-socket")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
```

## Pushing

A new `depot push` command on the CLI allows pushing of:

- Flake references: `depot push github:NixOS/nixpkgs/8cd5ce828d5d1d16feff37340171a98fc3bf6526#sl`
- Store paths: `depot push /nix/store/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05`
- A list of store paths and references from stdin: `depot push --stdin`

It simply starts the proxy on a random port (using port 0 does that):

```go
func main() {
	// Ask the OS for any free port by binding to ":0"
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}
	defer l.Close()

	addr := l.Addr().(*net.TCPAddr)
	fmt.Println("Random free port:", addr.Port)
}
```

Then, it orchestrates the correct nix commands to push to ensure that everything gets pushed, just like the `./integration` tests do. I suspect the `nixcmd` package could be converted from a test package and used.
