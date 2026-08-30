package handlers

import (
	"io/fs"
	"net/http"
	"os"
	"strings"

	"streamingestarr/static"
	"streamingestarr/utils"
	"streamingestarr/webserver/router/middleware"
)

// webAppFS returns the web app filesystem: an on-disk build when present
// (so UI updates don't require a binary restart — drop a fresh
// webapp/build into ./webapp-dist or point STREAMINGESTARR_WEBAPP_DIR at
// one), otherwise the build embedded in the binary.
func webAppFS() fs.FS {
	dir := os.Getenv("STREAMINGESTARR_WEBAPP_DIR")
	if dir == "" {
		dir = "webapp-dist"
	}
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return os.DirFS(dir)
	}
	return static.GetWebApp()
}

// ViewerAppHandler serves the Svelte app (viewer and admin pages).
func ViewerAppHandler(w http.ResponseWriter, r *http.Request) {
	middleware.EnableCors(w)

	isViewerRoute := r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/t/") ||
		r.URL.Path == "/admin" || strings.HasPrefix(r.URL.Path, "/admin/")

	// Media players pointed at the page get the stream directly.
	if utils.IsUserAgentAPlayer(r.UserAgent()) && isViewerRoute {
		http.Redirect(w, r, "/hls/stream.m3u8", http.StatusTemporaryRedirect)
		return
	}

	webApp := webAppFS()

	requestPath := strings.TrimPrefix(r.URL.Path, "/")
	if isViewerRoute || requestPath == "" {
		requestPath = "index.html"
	}

	if f, err := webApp.Open(requestPath); err == nil {
		_ = f.Close()
		middleware.SetCachingHeaders(w, r)
		http.ServeFileFS(w, r, webApp, requestPath)
		return
	}

	w.WriteHeader(http.StatusNotFound)
}

var _ fs.FS = static.GetWebApp()
