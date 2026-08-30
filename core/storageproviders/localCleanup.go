package storageproviders

import (
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func localCleanup(baseDirectory string, maxNumber int) error {

	files, err := getAllFilesRecursive(baseDirectory)
	if err != nil {
		return errors.Wrap(err, "unable find old video files for cleanup")
	}

	// Delete old private HLS files on disk
	for directory := range files {
		files := files[directory]
		if len(files) < maxNumber {
			continue
		}

		filesToDelete := files[maxNumber:]
		log.Traceln("Deleting", len(filesToDelete), "old files from", baseDirectory, "for video variant", directory)

		for _, file := range filesToDelete {
			fileToDelete := filepath.Join(baseDirectory, directory, file.Name())
			err := os.Remove(fileToDelete)
			if err != nil {
				return errors.Wrap(err, "unable to delete old video files")
			}
		}
	}
	return nil
}
