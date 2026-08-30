package core

import (
	"streamingestarr/core/storageproviders"
)

func (c *ChannelRuntime) setupStorage() error {
	c.storage = storageproviders.NewLocalStorage(c.HLSOutputPath)

	if err := c.storage.Setup(); err != nil {
		return err
	}

	c.handler.Storage = c.storage

	return nil
}
