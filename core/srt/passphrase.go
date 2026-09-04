package srt

import "streamingestarr/persistence/channelrepository"

// effectivePassphrase picks the passphrase a stream must present: the
// room's own when it has one, the global (per-protocol) one otherwise.
func effectivePassphrase(room, global string) string {
	if room != "" {
		return room
	}
	return global
}

// passphraseForKey resolves the passphrase for the room a (valid) stream
// key opens, falling back to the protocol's global passphrase.
func passphraseForKey(key, global string) string {
	return effectivePassphrase(channelrepository.PassphraseForKey(key), global)
}
