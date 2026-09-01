package core

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/grafov/m3u8"
	log "github.com/sirupsen/logrus"
	"streamingestarr/config"
	"streamingestarr/static"
	"streamingestarr/utils"
)

func appendOfflineToVariantPlaylist(index int, playlistFilePath string) {
	existingPlaylistContents, err := os.ReadFile(playlistFilePath) // nolint: gosec
	if err != nil {
		log.Debugln("unable to read existing playlist file", err)
		return
	}

	tmpFileName := fmt.Sprintf("tmp-stream-%d.m3u8", index)
	atomicWriteTmpPlaylistFile, err := os.CreateTemp(config.TempDir, tmpFileName)
	if err != nil {
		log.Errorln("error creating tmp playlist file to write to", playlistFilePath, err)
		return
	}

	// Write the existing playlist contents
	if _, err := atomicWriteTmpPlaylistFile.Write(existingPlaylistContents); err != nil {
		log.Debugln("error writing existing playlist contents to tmp playlist file", err)
		return
	}

	// Manually append the offline clip to the end of the media playlist.
	_, _ = atomicWriteTmpPlaylistFile.WriteString("#EXT-X-DISCONTINUITY\n")
	// If "offline" content gets changed then change the duration below
	_, _ = atomicWriteTmpPlaylistFile.WriteString("#EXTINF:8.000000,\n")
	_, _ = atomicWriteTmpPlaylistFile.WriteString("offline-v2.ts\n")
	_, _ = atomicWriteTmpPlaylistFile.WriteString("#EXT-X-ENDLIST\n")

	if err := atomicWriteTmpPlaylistFile.Close(); err != nil {
		log.Errorln(err)
	}

	if err := utils.Move(atomicWriteTmpPlaylistFile.Name(), playlistFilePath); err != nil {
		log.Errorln("error moving temp playlist to overwrite existing one", err)
	}
}

func (c *ChannelRuntime) makeVariantIndexOffline(index int, offlineFilePath string, offlineFilename string) {
	playlistFilePath := fmt.Sprintf(filepath.Join(c.HLSOutputPath, "%d/stream.m3u8"), index)
	segmentFilePath := fmt.Sprintf(filepath.Join(c.HLSOutputPath, "%d/%s"), index, offlineFilename)

	if err := utils.Copy(offlineFilePath, segmentFilePath); err != nil {
		log.Warnln(err)
	}

	if _, err := c.storage.Save(segmentFilePath, 0); err != nil {
		log.Warnln(err)
	}

	if utils.DoesFileExists(playlistFilePath) {
		appendOfflineToVariantPlaylist(index, playlistFilePath)
	} else {
		createEmptyOfflinePlaylist(playlistFilePath, offlineFilename)
	}
	if _, err := c.storage.Save(playlistFilePath, 0); err != nil {
		log.Warnln(err)
	}
}

func createEmptyOfflinePlaylist(playlistFilePath string, offlineFilename string) {
	p, err := m3u8.NewMediaPlaylist(1, 1)
	if err != nil {
		log.Errorln(err)
	}

	// If "offline" content gets changed then change the duration below
	if err := p.Append(offlineFilename, 8.0, ""); err != nil {
		log.Errorln(err)
	}

	p.Close()
	f, err := os.Create(playlistFilePath) //nolint:gosec
	if err != nil {
		log.Errorln(err)
	}
	defer f.Close()
	if _, err := f.Write(p.Encode().Bytes()); err != nil {
		log.Errorln(err)
	}
}

func saveOfflineClipToDisk(offlineFilename string) (string, error) {
	offlineFileData := static.GetOfflineSegment()

	// One fixed file, overwritten in place. The previous CreateTemp left a
	// new uniquely-suffixed copy behind on EVERY disconnect and offline
	// transition, slowly littering data/tmp forever; sweep those up once.
	if stale, err := filepath.Glob(filepath.Join(config.TempDir, offlineFilename+"*")); err == nil {
		for _, f := range stale {
			if filepath.Base(f) != offlineFilename {
				_ = os.Remove(f)
			}
		}
	}

	offlineFilePath := filepath.Join(config.TempDir, offlineFilename)
	if err := os.WriteFile(offlineFilePath, offlineFileData, 0o600); err != nil {
		return "", fmt.Errorf("unable to write offline segment to disk: %s", err)
	}
	return filepath.Abs(offlineFilePath)
}
