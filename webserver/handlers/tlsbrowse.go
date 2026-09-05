package handlers

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"streamingestarr/persistence/configrepository"
	"streamingestarr/webserver/utils"
)

// Browsing for the TLS certificate pair. The paths the admin types are
// paths inside the container, which a bind mount may or may not have
// produced — so the admin page lists what the container actually sees,
// and whether it can read it (the ACL trap from docs/deploy-vps.md).

type browseEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size"`
	// PEM: the file looks like a certificate or key, by content or name.
	PEM bool `json:"pem"`
	// Readable: the container can open it — for a directory, list it.
	Readable bool `json:"readable"`
}

type browseResponse struct {
	Path    string        `json:"path"`
	Parent  string        `json:"parent"`
	Entries []browseEntry `json:"entries"`
	Error   string        `json:"error,omitempty"`
}

// browseLimit caps a listing; a certificate store is never this big.
const browseLimit = 500

// BrowseTCPIngestTLS lists one directory inside the container:
// GET /api/admin/config/tcp/tls/browse?path=/certs. Without a path it
// starts where the saved certificate lives, else at /certs.
func BrowseTCPIngestTLS(w http.ResponseWriter, r *http.Request) {
	dir := strings.TrimSpace(r.URL.Query().Get("path"))
	if dir == "" {
		dir = defaultBrowseDir()
	}
	dir = filepath.Clean("/" + dir)
	resp := browseResponse{Path: dir, Parent: filepath.Dir(dir), Entries: []browseEntry{}}

	entries, err := os.ReadDir(dir)
	if err != nil {
		resp.Error = browseErrorText(dir, err)
		utils.WriteResponse(w, resp)
		return
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		be := browseEntry{Name: e.Name(), Path: p, Dir: e.IsDir()}
		// A symlink counts as what it points to.
		if st, err := os.Stat(p); err == nil {
			be.Dir = st.IsDir()
			if !be.Dir {
				be.Size = st.Size()
			}
		}
		if be.Dir {
			_, err := os.ReadDir(p)
			be.Readable = err == nil
		} else {
			be.PEM, be.Readable = pemFile(p)
		}
		resp.Entries = append(resp.Entries, be)
		if len(resp.Entries) >= browseLimit {
			break
		}
	}
	sort.SliceStable(resp.Entries, func(i, j int) bool {
		if resp.Entries[i].Dir != resp.Entries[j].Dir {
			return resp.Entries[i].Dir
		}
		return strings.ToLower(resp.Entries[i].Name) < strings.ToLower(resp.Entries[j].Name)
	})
	utils.WriteResponse(w, resp)
}

// defaultBrowseDir is the saved certificate's folder when it exists,
// else /certs — present or not, so a missing mount is the first thing
// the panel says.
func defaultBrowseDir() string {
	if cert := configrepository.Get().GetTCPIngestTLSCertFile(); cert != "" {
		if st, err := os.Stat(filepath.Dir(cert)); err == nil && st.IsDir() {
			return filepath.Dir(cert)
		}
	}
	return "/certs"
}

// pemFile says whether a file looks like PEM (by its first bytes, or by
// its name when it cannot be read) and whether it could be opened.
func pemFile(p string) (pem bool, readable bool) {
	byName := false
	switch strings.ToLower(filepath.Ext(p)) {
	case ".crt", ".pem", ".key", ".cer":
		byName = true
	}
	f, err := os.Open(p)
	if err != nil {
		return byName, false
	}
	defer f.Close()
	head := make([]byte, 128)
	n, _ := f.Read(head)
	return byName || bytes.Contains(head[:n], []byte("-----BEGIN")), true
}

// browseErrorText turns the two errors that actually happen into the
// sentence that fixes them.
func browseErrorText(dir string, err error) string {
	switch {
	case os.IsNotExist(err):
		if dir == "/certs" {
			return "There is no /certs in the container — the certificate volume is not mounted. Add it under volumes: in docker-compose.yml (or docker-compose.override.yml) and run docker compose up -d; a restart alone keeps the old mounts."
		}
		return dir + " does not exist inside the container."
	case os.IsPermission(err):
		return "The container (uid 101) may not read " + dir + " — grant it access with the setfacl commands in docs/deploy-vps.md."
	}
	return err.Error()
}
