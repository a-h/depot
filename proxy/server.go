package proxy

import (
	"crypto"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/a-h/depot/auth"
	"golang.org/x/crypto/ssh"
)

// Server represents a proxy server that adds authentication headers.
type Server struct {
	log      *slog.Logger
	target   *url.URL
	proxy    *httputil.ReverseProxy
	jwtToken string
}

// NewServer creates a new proxy server that forwards requests to the target URL.
func NewServer(log *slog.Logger, targetURL string) (*Server, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid target URL: %w", err)
	}

	// Create JWT token from available SSH keys.
	jwtToken, err := createJWTFromSSHKeys(log)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT token: %w", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	s := &Server{
		log:      log,
		target:   target,
		proxy:    proxy,
		jwtToken: jwtToken,
	}

	// Customize the proxy to add authentication headers.
	proxy.Director = func(req *http.Request) {
		// Standard reverse proxy setup.
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.Host = target.Host

		// Add the JWT token as Bearer authorization.
		req.Header.Set("Authorization", "Bearer "+jwtToken)
	}

	return s, nil
}

// ServeHTTP implements the http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Log the request.
	s.log.Info("proxy request", slog.String("method", r.Method), slog.String("path", r.URL.Path))

	// Forward the request to the target.
	s.proxy.ServeHTTP(w, r)
}

// StartProxy starts a proxy server on a random port and returns the address.
func StartProxy(log *slog.Logger, targetURL string) (string, func(), error) {
	return StartProxyOnPort(log, targetURL, 0)
}

// StartProxyOnPort starts a proxy server on the specified port and returns the address.
func StartProxyOnPort(log *slog.Logger, targetURL string, port int) (string, func(), error) {
	server, err := NewServer(log, targetURL)
	if err != nil {
		return "", nil, err
	}

	// Listen on the specified port (or random if port is 0).
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, fmt.Errorf("failed to start listener on port %d: %w", port, err)
	}

	actualAddr := listener.Addr().String()
	httpServer := &http.Server{
		Handler: server,
	}

	// Start serving in a goroutine.
	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Error("proxy server error", slog.String("error", err.Error()))
		}
	}()

	// Return the address and a cleanup function.
	cleanup := func() {
		httpServer.Close()
	}

	return actualAddr, cleanup, nil
}

// createJWTFromSSHKeys discovers SSH keys and creates a JWT token.
func createJWTFromSSHKeys(log *slog.Logger) (string, error) {
	keys, err := auth.DiscoverSSHKeys()
	if err != nil {
		return "", fmt.Errorf("failed to discover SSH keys: %w", err)
	}

	if len(keys) == 0 {
		return "", fmt.Errorf("no SSH keys found")
	}

	// Try to find a key that can be used for signing.
	for _, keyInfo := range keys {
		if keyInfo.Signer == nil {
			log.Debug("skipping key without signer", slog.String("fingerprint", keyInfo.Fingerprint))
			continue
		}

		// Check if this is a supported key type for JWT signing.
		pubKey := keyInfo.Signer.PublicKey()
		if !isSupportedKeyType(pubKey) {
			log.Debug("skipping unsupported key type", slog.String("type", pubKey.Type()), slog.String("fingerprint", keyInfo.Fingerprint))
			continue
		}

		// Try to create a crypto.Signer from the SSH signer.
		cryptoSigner, err := sshSignerToCryptoSigner(keyInfo.Signer)
		if err != nil {
			log.Debug("failed to convert SSH signer to crypto signer", slog.String("error", err.Error()), slog.String("fingerprint", keyInfo.Fingerprint))
			continue
		}

		// Create JWT token.
		token, err := auth.CreateJWT(cryptoSigner, pubKey)
		if err != nil {
			log.Debug("failed to create JWT", slog.String("error", err.Error()), slog.String("fingerprint", keyInfo.Fingerprint))
			continue
		}

		log.Info("using SSH key for authentication", slog.String("fingerprint", keyInfo.Fingerprint), slog.String("source", keyInfo.Source))
		return token, nil
	}

	return "", fmt.Errorf("no usable SSH keys found for JWT signing")
}

// isSupportedKeyType checks if the SSH key type is supported for JWT signing.
func isSupportedKeyType(pubKey ssh.PublicKey) bool {
	switch pubKey.Type() {
	case ssh.KeyAlgoRSA, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		return true
	default:
		return false
	}
}

// sshSignerToCryptoSigner converts an SSH signer to a crypto.Signer.
func sshSignerToCryptoSigner(sshSigner ssh.Signer) (crypto.Signer, error) {
	// For SSH signers, we need to create a wrapper.
	return &cryptoSignerWrapper{sshSigner: sshSigner}, nil
}

// cryptoSignerWrapper wraps an SSH signer to implement crypto.Signer.
type cryptoSignerWrapper struct {
	sshSigner ssh.Signer
}

func (w *cryptoSignerWrapper) Public() crypto.PublicKey {
	// Extract the crypto public key from the SSH public key.
	sshPubKey := w.sshSigner.PublicKey()
	cryptoPubKey, err := auth.ExtractCryptoPublicKey(sshPubKey)
	if err != nil {
		// This shouldn't happen if we validated the key type earlier.
		panic(fmt.Sprintf("failed to extract crypto public key: %v", err))
	}
	return cryptoPubKey
}

func (w *cryptoSignerWrapper) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	// Use the SSH signer to sign.
	signature, err := w.sshSigner.Sign(rand, digest)
	if err != nil {
		return nil, err
	}
	return signature.Blob, nil
}
