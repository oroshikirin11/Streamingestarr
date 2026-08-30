package handlers

import (
	"io/fs"
	"net/http"
	"strings"

	"streamingestarr/static"
	"streamingestarr/utils"
	"streamingestarr/webserver/router/middleware"
)

// ViewerAppHandler serves the Svelte viewer app for the theater pages, and
// falls back to the legacy web bundle for everything else it still owns
// (the /admin app and its assets, images, fonts).
func ViewerAppHandler(w http.ResponseWriter, r *http.Request) {
	middleware.EnableCors(w)

	isViewerRoute := r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/t/") ||
		r.URL.Path == "/admin" || strings.HasPrefix(r.URL.Path, "/admin/")

	// Media players pointed at the page get the stream directly.
	if utils.IsUserAgentAPlayer(r.UserAgent()) && isViewerRoute {
		http.Redirect(w, r, "/hls/stream.m3u8", http.StatusTemporaryRedirect)
		return
	}

	webApp := static.GetWebApp()

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

	// Not a viewer-app file: the legacy bundle may own it (/admin assets
	// under /_next/, shared images, fonts).
	IndexHandler(w, r)
}

var _ fs.FS = static.GetWebApp()
