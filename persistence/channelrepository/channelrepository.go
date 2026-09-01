// Package channelrepository stores the channel (theater) list. The default
// channel always exists; additional rooms are plain data rows, each carrying
// its own stream key — the key is how an inbound stream picks its room, so
// no extra ports ever open.
package channelrepository

import (
	"database/sql"
	"errors"
	"regexp"

	log "github.com/sirupsen/logrus"

	"streamingestarr/models"
)

// DefaultChannelID is the channel every fresh install gets, and the one
// legacy unscoped URLs (/hls/stream.m3u8) resolve to.
const DefaultChannelID = "main"

// MaxChannels caps how many rooms can exist at once. Passthrough keeps the
// CPU cost of a room near zero, but every live room still carries a
// transcoder process and an ingest session — five is the designed ceiling.
const MaxChannels = 5

var _db *sql.DB

// ValidChannelID constrains IDs to something URL- and directory-safe.
var ValidChannelID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Setup creates the channels table and seeds the default channel.
func Setup(db *sql.DB) {
	_db = db
	createTableSQL := `CREATE TABLE IF NOT EXISTS channels (
		"id" TEXT NOT NULL PRIMARY KEY,
		"name" TEXT NOT NULL,
		"created_at" DATE DEFAULT CURRENT_TIMESTAMP NOT NULL
	);`
	if _, err := db.Exec(createTableSQL); err != nil {
		log.Fatalln("unable to create channels table:", err)
	}
	// Rooms carry their own stream keys — as many as they like, mirroring
	// the global list (which stays the default channel's key store).
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS channel_keys (
		"key" TEXT NOT NULL PRIMARY KEY,
		"channel_id" TEXT NOT NULL,
		"comment" TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		log.Fatalln("unable to create channel_keys table:", err)
	}
	// Migration from the short-lived single-key column: carry the key over,
	// then leave the column dormant.
	if columnExists(db, "channels", "stream_key") {
		if _, err := db.Exec(`INSERT OR IGNORE INTO channel_keys(key, channel_id)
			SELECT stream_key, id FROM channels WHERE stream_key != ''`); err != nil {
			log.Errorln("unable to migrate room keys:", err)
		}
		if _, err := db.Exec(`UPDATE channels SET stream_key = '' WHERE stream_key != ''`); err != nil {
			log.Errorln("unable to clear migrated room keys:", err)
		}
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO channels(id, name) VALUES(?, ?)`,
		DefaultChannelID, "Main Theater"); err != nil {
		log.Fatalln("unable to seed default channel:", err)
	}
}

func columnExists(db *sql.DB, table, column string) bool {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil && name == column {
			return true
		}
	}
	return false
}

// GetChannel returns a channel by ID (keys included), or nil if it does
// not exist.
func GetChannel(id string) *models.Channel {
	var c models.Channel
	row := _db.QueryRow("SELECT id, name FROM channels WHERE id = ?", id)
	if err := row.Scan(&c.ID, &c.Name); err != nil {
		return nil
	}
	c.Keys = ListChannelKeys(id)
	return &c
}

// ListChannels returns all channels with their keys, oldest first.
func ListChannels() []models.Channel {
	rows, err := _db.Query("SELECT id, name FROM channels ORDER BY created_at")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var channels []models.Channel
	for rows.Next() {
		var c models.Channel
		if err := rows.Scan(&c.ID, &c.Name); err == nil {
			channels = append(channels, c)
		}
	}
	rows.Close()
	for i := range channels {
		channels[i].Keys = ListChannelKeys(channels[i].ID)
	}
	return channels
}

// ListChannelKeys returns a room's stream keys.
func ListChannelKeys(channelID string) []models.ChannelKey {
	rows, err := _db.Query("SELECT key, comment FROM channel_keys WHERE channel_id = ? ORDER BY key", channelID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var keys []models.ChannelKey
	for rows.Next() {
		var k models.ChannelKey
		if err := rows.Scan(&k.Key, &k.Comment); err == nil {
			keys = append(keys, k)
		}
	}
	return keys
}

// CountChannels returns how many channels exist.
func CountChannels() int {
	var n int
	if err := _db.QueryRow("SELECT COUNT(*) FROM channels").Scan(&n); err != nil {
		return 0
	}
	return n
}

// GetChannelIDForKey resolves a stream key to the room that owns it, or ""
// when no room claims it (the caller then checks the default channel's
// global key list). This is THE routing lookup: it is what lets five rooms
// share three ingest ports.
func GetChannelIDForKey(key string) string {
	if key == "" {
		return ""
	}
	var id string
	row := _db.QueryRow("SELECT channel_id FROM channel_keys WHERE key = ?", key)
	if err := row.Scan(&id); err != nil {
		return ""
	}
	return id
}

// AddChannel inserts a new room with its first stream key. The MaxChannels
// cap and runtime creation live in core; this only persists the rows.
func AddChannel(id, name, streamKey string) error {
	if !ValidChannelID.MatchString(id) {
		return errors.New("invalid channel id")
	}
	if id == DefaultChannelID {
		return errors.New("channel already exists")
	}
	if streamKey == "" {
		return errors.New("a room needs a stream key")
	}
	res, err := _db.Exec(`INSERT OR IGNORE INTO channels(id, name) VALUES(?, ?)`, id, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("channel already exists")
	}
	if _, err := _db.Exec(`INSERT OR IGNORE INTO channel_keys(key, channel_id) VALUES(?, ?)`, streamKey, id); err != nil {
		return err
	}
	return nil
}

// DeleteChannel removes a room and its keys. The default channel is not
// deletable.
func DeleteChannel(id string) error {
	if id == DefaultChannelID {
		return errors.New("the default channel cannot be deleted")
	}
	res, err := _db.Exec(`DELETE FROM channels WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("no such channel")
	}
	_, _ = _db.Exec(`DELETE FROM channel_keys WHERE channel_id = ?`, id)
	return nil
}

// SetChannelName renames a room.
func SetChannelName(id, name string) error {
	_, err := _db.Exec(`UPDATE channels SET name = ? WHERE id = ?`, name, id)
	return err
}

// ReplaceChannelKeys sets a room's full key list — the same edit-then-save
// shape the global list uses. The default channel's keys live in the global
// list, not here. A key already owned by ANOTHER room is rejected: keys are
// the router, so they must be unambiguous.
func ReplaceChannelKeys(id string, keys []models.ChannelKey) error {
	if id == DefaultChannelID {
		return errors.New("the default channel's keys are managed in the stream settings")
	}
	if len(keys) == 0 {
		return errors.New("a room needs at least one stream key")
	}
	for _, k := range keys {
		if k.Key == "" {
			return errors.New("a stream key cannot be empty")
		}
		if owner := GetChannelIDForKey(k.Key); owner != "" && owner != id {
			return errors.New("the key " + k.Key + " already belongs to another room")
		}
	}
	tx, err := _db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() // nolint
	if _, err := tx.Exec(`DELETE FROM channel_keys WHERE channel_id = ?`, id); err != nil {
		return err
	}
	for _, k := range keys {
		if _, err := tx.Exec(`INSERT INTO channel_keys(key, channel_id, comment) VALUES(?, ?, ?)`, k.Key, id, k.Comment); err != nil {
			return err
		}
	}
	return tx.Commit()
}
