package static

import (
	"embed"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

//go:embed all:webapp/*
var webAppFiles embed.FS

// GetWebApp returns the embedded Svelte viewer app.
func GetWebApp() fs.FS {
	wf, err := fs.Sub(webAppFiles, "webapp")
	if err != nil {
		log.Fatal(err)
	}
	return wf
}

//go:embed img/emoji/*
var emojiFiles embed.FS

// GetEmoji will return the emoji files.
func GetEmoji() fs.FS {
	ef, err := fs.Sub(emojiFiles, "img/emoji")
	if err != nil {
		log.Fatal(err)
	}
	return ef
}

//go:embed offline-v2.ts
var offlineVideoSegment []byte

// GetOfflineSegment will return the offline video segment data.
func GetOfflineSegment() []byte {
	return getFileSystemStaticFileOrDefault("offline-v2.ts", offlineVideoSegment)
}

//go:embed img/logo.png
var logo []byte

// GetLogo will return the logo data.
func GetLogo() []byte {
	return getFileSystemStaticFileOrDefault("img/logo.png", logo)
}

//go:embed favicon.png
var favicon []byte

// GetFavicon will return the favicon data.
func GetFavicon() []byte {
	return getFileSystemStaticFileOrDefault("favicon.png", favicon)
}

func getFileSystemStaticFileOrDefault(path string, defaultData []byte) []byte {
	fullPath := filepath.Join("static", path)
	data, err := os.ReadFile(fullPath) //nolint: gosec
	if err != nil {
		return defaultData
	}

	return data
}
