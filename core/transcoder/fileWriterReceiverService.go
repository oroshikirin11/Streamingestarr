package transcoder

import (
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
	"streamingestarr/config"
)

// FileWriterReceiverServiceCallback are to be fired when transcoder responses are written to disk.
type FileWriterReceiverServiceCallback interface {
	SegmentWritten(localFilePath string)
	VariantPlaylistWritten(localFilePath string)
	MasterPlaylistWritten(localFilePath string)
}

// FileWriterReceiverService accepts transcoder responses via HTTP and fires the callbacks.
// It is intended to be the middleman between the transcoder and the storage provider and allows
// the transcoder process to be completely isolated and even run remotely in the future, as long
// as it can send HTTP requests to this service with the results.
// One instance runs per channel, each writing into that channel's HLS
// directory on its own local port.
type FileWriterReceiverService struct {
	callbacks FileWriterReceiverServiceCallback
	basePath  string
	port      string
}

// SetupFileWriterReceiverService will start listening for transcoder
// responses and write them under basePath (the channel's HLS directory).
func (s *FileWriterReceiverService) SetupFileWriterReceiverService(callbacks FileWriterReceiverServiceCallback, basePath string) {
	s.callbacks = callbacks
	s.basePath = basePath

	httpServer := http.NewServeMux()
	httpServer.HandleFunc("/", s.uploadHandler)

	localListenerAddress := "127.0.0.1:0"

	listener, err := net.Listen("tcp", localListenerAddress)
	if err != nil {
		log.Fatalln("Unable to start internal video writing service", err)
	}

	s.port = strings.Split(listener.Addr().String(), ":")[1]
	// Kept for anything still reading the global default; per-channel
	// transcoders are handed Port() explicitly.
	config.InternalHLSListenerPort = s.port
	log.Traceln("Transcoder response service listening on: " + s.port)
	go func() {
		//nolint: gosec
		if err := http.Serve(listener, httpServer); err != nil {
			log.Fatalln("Unable to start internal video writing service", err)
		}
	}()
}

// Port returns the local port this receiver listens on.
func (s *FileWriterReceiverService) Port() string {
	return s.port
}

func (s *FileWriterReceiverService) uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	path := r.URL.Path
	writePath := filepath.Join(s.basePath, path)
	f, err := os.Create(writePath) //nolint: gosec
	if err != nil {
		returnError(err, w)
		return
	}

	defer f.Close()

	if _, err := io.Copy(f, r.Body); err != nil {
		returnError(err, w)
		return
	}

	s.fileWritten(writePath)
	w.WriteHeader(http.StatusOK)
}

func (s *FileWriterReceiverService) fileWritten(path string) {
	if path == filepath.Join(s.basePath, "stream.m3u8") {
		s.callbacks.MasterPlaylistWritten(path)
	} else if strings.HasSuffix(path, ".ts") {
		s.callbacks.SegmentWritten(path)
	} else if strings.HasSuffix(path, ".m3u8") {
		s.callbacks.VariantPlaylistWritten(path)
	}
}

func returnError(err error, w http.ResponseWriter) {
	log.Debugln(err)
	http.Error(w, http.StatusText(http.StatusInternalServerError)+": "+err.Error(), http.StatusInternalServerError)
}
