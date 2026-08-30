package handlers

import (
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"streamingestarr/core"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/utils"
	"streamingestarr/webserver/router/middleware"
)

// HandleHLSRequest will manage all requests to HLS content.
//
// URLs are channel-scoped: /hls/<channel>/... — and for compatibility with
// clients that predate channels, /hls/<file> resolves to the default
// channel (the master playlist references its variants relatively, so
// those arrive unscoped too).
func HandleHLSRequest(w http.ResponseWriter, r *http.Request) {
	// Sanity check to limit requests to HLS file types.
	if filepath.Ext(r.URL.Path) != ".m3u8" && filepath.Ext(r.URL.Path) != ".ts" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	relativePath := strings.Replace(r.URL.Path, "/hls/", "", 1)

	// A first path segment naming an existing channel scopes the request;
	// anything else belongs to the default channel.
	channelID := channelrepository.DefaultChannelID
	if first, rest, found := strings.Cut(relativePath, "/"); found && channelrepository.GetChannel(first) != nil {
		channelID = first
		relativePath = rest
	}

	channel := core.GetChannelRuntime(channelID)
	if channel == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	fullPath := filepath.Join(channel.HLSOutputPath, relativePath)

	// Handle playlists
	if path.Ext(r.URL.Path) == ".m3u8" {
		// Playlists should never be cached.
		middleware.DisableCache(w)

		// Force the correct content type
		w.Header().Set("Content-Type", "application/x-mpegURL")

		// Use this as an opportunity to mark this viewer as active.
		viewer := models.GenerateViewerFromRequest(r)
		channel.SetViewerActive(&viewer)
	} else {
		cacheTime := utils.GetCacheDurationSecondsForPath(relativePath)
		w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(cacheTime))
	}

	middleware.EnableCors(w)
	http.ServeFile(w, r, fullPath)
}
