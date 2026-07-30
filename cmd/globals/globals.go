package globals

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Globals holds flags that apply to all commands.
type Globals struct {
	Verbose            bool          `help:"Enable verbose logging" short:"v" default:"false"`
	InsecureSkipVerify bool          `help:"Skip TLS certificate verification (insecure)" default:"false"`
	Timeout            time.Duration `help:"Timeout for individual HTTP requests" default:"15m"`
}

// NewRoundTripper returns a transport configured with the global flags. It
// clones http.DefaultTransport so connection-pool settings are preserved.
func (g *Globals) NewRoundTripper() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = g.Timeout
	if g.InsecureSkipVerify {
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	return t
}

// NewHTTPClient returns an HTTP client configured with the global flags.
func (g *Globals) NewHTTPClient() *http.Client {
	return &http.Client{Timeout: g.Timeout, Transport: g.NewRoundTripper()}
}

// NewContext returns a context that is cancelled when the process receives an
// interrupt or termination signal, and a function to release the signal handler.
func (g *Globals) NewContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
