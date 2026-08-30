package core

import (
	"github.com/owncast/owncast/core/storageproviders"
)

func setupStorage() error {
	_storage = storageproviders.NewLocalStorage()

	if err := _storage.Setup(); err != nil {
		return err
	}

	handler.Storage = _storage

	return nil
}
