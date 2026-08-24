package proxy

import (
	"fmt"
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

// NewServer creates a new proxy server that forwards requests to the target URL. If identity is set,
// it is the path to the SSH key to authenticate with, otherwise the first usable key is used.
func NewServer(log *slog.Logger, targetURL string, transport http.RoundTripper, identity string) (*Server, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid target URL: %w", err)
	}

	// Create JWT token from available SSH keys.
	jwtToken, err := createJWTFromSSHKeys(log, identity)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT token: %w", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = transport

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
func StartProxy(log *slog.Logger, targetURL string, transport http.RoundTripper, identity string) (string, func(), error) {
	return StartProxyOnPort(log, targetURL, transport, 0, identity)
}

// StartProxyOnPort starts a proxy server on the specified port and returns the address.
func StartProxyOnPort(log *slog.Logger, targetURL string, transport http.RoundTripper, port int, identity string) (string, func(), error) {
	server, err := NewServer(log, targetURL, transport, identity)
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
func createJWTFromSSHKeys(log *slog.Logger, identity string) (string, error) {
	if identity != "" {
		signer, err := auth.LoadIdentity(log, identity)
		if err != nil {
			return "", err
		}
		if !auth.IsSupportedKeyType(signer.PublicKey()) {
			return "", fmt.Errorf("identity %s uses unsupported key type %s", identity, signer.PublicKey().Type())
		}
		log.Info("using SSH key for authentication", slog.String("fingerprint", ssh.FingerprintSHA256(signer.PublicKey())), slog.String("identity", identity))
		return auth.CreateJWT(signer)
	}

	keys, err := auth.DiscoverSSHKeys(log)
	if err != nil {
		return "", fmt.Errorf("failed to discover SSH keys: %w", err)
	}

	if len(keys) == 0 {
		return "", fmt.Errorf("no SSH keys found")
	}

	log.Debug("searching key files", slog.Any("keys", keys))

	// Try to find a key that can be used for signing.
	for _, keyInfo := range keys {
		if keyInfo.Signer == nil {
			log.Debug("skipping key without signer", slog.String("fingerprint", keyInfo.Fingerprint))
			continue
		}

		// Check if this is a supported key type for JWT signing.
		pubKey := keyInfo.Signer.PublicKey()
		if !auth.IsSupportedKeyType(pubKey) {
			log.Debug("skipping unsupported key type", slog.String("type", pubKey.Type()), slog.String("fingerprint", keyInfo.Fingerprint))
			continue
		}

		// Create JWT token.
		token, err := auth.CreateJWT(keyInfo.Signer)
		if err != nil {
			log.Debug("failed to create JWT", slog.String("error", err.Error()), slog.String("fingerprint", keyInfo.Fingerprint))
			continue
		}

		log.Info("using SSH key for authentication", slog.String("fingerprint", keyInfo.Fingerprint), slog.String("source", keyInfo.Source))
		return token, nil
	}

	return "", fmt.Errorf("no usable SSH keys found for JWT signing")
}
