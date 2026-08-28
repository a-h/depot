package web

import (
	"bytes"
	"embed"
	"log/slog"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

//go:embed content
var contentFS embed.FS

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
)

type handler struct {
	log *slog.Logger
}

// New creates an HTTP handler serving the documentation web UI.
func New(log *slog.Logger) (http.Handler, error) {
	return &handler{log: log}, nil
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for _, item := range navItems {
		if r.URL.Path == item.Path {
			h.servePage(w, r, item)
			return
		}
	}
	http.NotFound(w, r)
}

func (h *handler) servePage(w http.ResponseWriter, r *http.Request, item navItem) {
	src, err := contentFS.ReadFile(item.File)
	if err != nil {
		h.log.Error("failed to read doc file", slog.String("file", item.File), slog.Any("error", err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	baseURL := getRequestBaseURL(r)
	replaced := strings.ReplaceAll(string(src), "{{.BaseURL}}", baseURL)

	var buf bytes.Buffer
	if err = md.Convert([]byte(replaced), &buf); err != nil {
		h.log.Error("failed to render markdown", slog.Any("error", err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	templ.Handler(docPage(item.Title, item.Path, buf.String())).ServeHTTP(w, r)
}

func getRequestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + r.Host
}
