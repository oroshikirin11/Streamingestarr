package handlers

import (
	"bufio"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"streamingestarr/core"
	"streamingestarr/models"
	"streamingestarr/persistence/channelrepository"
	"streamingestarr/webserver/router/middleware"
)

// The HTTP relay outlets, outside the viewer gate — the token in the
// link is the key:
//
//	/relay/<room>/<token>.ts          MPEG-TS over HTTP
//	/relay/<room>/<token>.m3u8        HLS master, variants rewritten below
//	/relay/<room>/<token>/<hls path>  HLS playlists and segments
func HandleRelayRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/relay/")
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	ch := channelrepository.GetChannel(parts[0])
	ext := path.Ext(parts[1])
	token := strings.TrimSuffix(parts[1], ext)
	// Everything that is not a live link is the same 404 — no probing.
	if ch == nil || !core.RelayTokenMatches(ch, token) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	channel := core.GetChannelRuntime(ch.ID)
	if channel == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	has := func(p string) bool {
		for _, x := range ch.EffectiveRelayProtocols() {
			if x == p {
				return true
			}
		}
		return false
	}
	middleware.DisableCache(w)

	switch {
	case len(parts) == 2 && ext == ".ts" && has(models.RelayProtocolTS):
		channel.ServeRelayTS(w, r)
	case len(parts) == 2 && ext == ".m3u8" && has(models.RelayProtocolHLS):
		serveRelayMaster(w, r, channel, ch.ID, token)
	case len(parts) == 3 && ext == "" && has(models.RelayProtocolHLS):
		serveRelayHLSFile(w, r, channel, parts[2])
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// serveRelayMaster rewrites the room's HLS master so its variants point
// back through the tokenised path.
func serveRelayMaster(w http.ResponseWriter, r *http.Request, channel *core.ChannelRuntime, room, token string) {
	f, err := os.Open(filepath.Join(channel.HLSOutputPath, "stream.m3u8"))
	if err != nil {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "the stream is not live", http.StatusServiceUnavailable)
		return
	}
	defer f.Close()
	channel.MarkRelayHLSPlayer(playerKey(r))
	w.Header().Set("Content-Type", "application/x-mpegURL")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	prefix := "/relay/" + room + "/" + token + "/"
	sc := bufio.NewScanner(f)
	var out strings.Builder
	for sc.Scan() {
		line := sc.Text()
		if line != "" && !strings.HasPrefix(line, "#") && !strings.Contains(line, "://") {
			line = prefix + strings.TrimPrefix(line, "/")
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	_, _ = w.Write([]byte(out.String()))
}

// serveRelayHLSFile serves one playlist or segment of the room's HLS
// output; variant playlists reference their segments relatively, so
// they resolve back here on their own.
func serveRelayHLSFile(w http.ResponseWriter, r *http.Request, channel *core.ChannelRuntime, rel string) {
	ext := filepath.Ext(rel)
	if ext != ".m3u8" && ext != ".ts" && ext != ".m4s" && ext != ".mp4" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	clean := filepath.Clean("/" + rel)
	full := filepath.Join(channel.HLSOutputPath, clean)
	if !strings.HasPrefix(full, filepath.Clean(channel.HLSOutputPath)+string(filepath.Separator)) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if ext == ".m3u8" {
		channel.MarkRelayHLSPlayer(playerKey(r))
		w.Header().Set("Content-Type", "application/x-mpegURL")
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeFile(w, r, full)
}

// playerKey tells HLS pullers apart: address and user agent.
func playerKey(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host + "|" + r.UserAgent()
}
