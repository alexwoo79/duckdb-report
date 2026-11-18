package preview

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/alexwoo79/duckdb-report/internal/workspace"
)

type Handler struct {
	storeProvider func() *workspace.Store
}

func NewHandler(storeProvider func() *workspace.Store) *Handler {
	return &Handler{storeProvider: storeProvider}
}

func (h *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	const prefix = "/preview/"
	if !strings.HasPrefix(request.URL.Path, prefix) {
		http.NotFound(response, request)
		return
	}
	remainder := strings.TrimPrefix(request.URL.Path, prefix)
	parts := strings.SplitN(remainder, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.NotFound(response, request)
		return
	}
	store := h.storeProvider()
	if store == nil {
		http.NotFound(response, request)
		return
	}
	runDir, err := store.RunDir(parts[0])
	if err != nil {
		http.NotFound(response, request)
		return
	}
	relative := filepath.Clean(filepath.FromSlash(parts[1]))
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		http.NotFound(response, request)
		return
	}
	path := filepath.Join(runDir, relative)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		http.NotFound(response, request)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(response, request, path)
}
