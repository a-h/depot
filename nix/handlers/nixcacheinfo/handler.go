package nixcacheinfo

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/nix-community/go-nix/pkg/narinfo/signature"
)

func New(log *slog.Logger, privateKey *signature.SecretKey) Handler {
	return Handler{
		log:        log,
		privateKey: privateKey,
	}
}

type Handler struct {
	log        *slog.Logger
	privateKey *signature.SecretKey
}

const CacheInfo = `StoreDir: /nix/store
WantMassQuery: 1
Priority: 30
`

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, CacheInfo)

	// Add public key if we have a private key.
	if h.privateKey != nil {
		publicKey := h.privateKey.ToPublicKey()
		fmt.Fprintf(w, "PublicKey: %s\n", publicKey.String())
	}
}
