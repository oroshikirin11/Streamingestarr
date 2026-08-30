package storageproviders

import (
	"bufio"
	"os"
	"path/filepath"

	"github.com/grafov/m3u8"
	"streamingestarr/core/playlist"

	log "github.com/sirupsen/logrus"
)

// rewritePlaylistLocations will take a local playlist and rewrite it to have absolute URLs to a specified location.
func rewritePlaylistLocations(localFilePath, remoteServingEndpoint, hlsPrefix, baseDir string) error {
	f, err := os.Open(localFilePath) // nolint
	if err != nil {
		log.Fatalln(err)
	}

	p := m3u8.NewMasterPlaylist()
	if err := p.DecodeFrom(bufio.NewReader(f), false); err != nil {
		log.Warnln(err)
	}

	if hlsPrefix == "" {
		hlsPrefix = "hls"
	}

	for _, item := range p.Variants {
		item.URI = remoteServingEndpoint + "/" + hlsPrefix + "/" + item.URI
	}

	publicPath := filepath.Join(baseDir, filepath.Base(localFilePath))

	newPlaylist := p.String()

	return playlist.WritePlaylist(newPlaylist, publicPath)
}
