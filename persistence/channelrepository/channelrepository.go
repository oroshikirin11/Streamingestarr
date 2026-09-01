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
	// Migration: rooms carry their own stream key. The default channel keeps
	// "" here — its keys are the pre-existing global list, untouched.
	if !columnExists(db, "channels", "stream_key") {
		if _, err := db.Exec(`ALTER TABLE channels ADD COLUMN stream_key TEXT NOT NULL DEFAULT ''`); err != nil {
			log.Fatalln("unable to add stream_key column to channels:", err)
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

// GetChannel returns a channel by ID, or nil if it does not exist.
func GetChannel(id string) *models.Channel {
	var c models.Channel
	row := _db.QueryRow("SELECT id, name, stream_key FROM channels WHERE id = ?", id)
	if err := row.Scan(&c.ID, &c.Name, &c.StreamKey); err != nil {
		return nil
	}
	return &c
}

// ListChannels returns all channels, oldest first.
func ListChannels() []models.Channel {
	rows, err := _db.Query("SELECT id, name, stream_key FROM channels ORDER BY created_at")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var channels []models.Channel
	for rows.Next() {
		var c models.Channel
		if err := rows.Scan(&c.ID, &c.Name, &c.StreamKey); err == nil {
			channels = append(channels, c)
		}
	}
	return channels
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
	row := _db.QueryRow("SELECT id FROM channels WHERE stream_key = ? AND stream_key != ''", key)
	if err := row.Scan(&id); err != nil {
		return ""
	}
	return id
}

// AddChannel inserts a new room with its own stream key. The MaxChannels
// cap and runtime creation live in core; this only persists the row.
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
	res, err := _db.Exec(`INSERT OR IGNORE INTO channels(id, name, stream_key) VALUES(?, ?, ?)`, id, name, streamKey)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("channel already exists")
	}
	return nil
}

// DeleteChannel removes a room. The default channel is not deletable.
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
	return nil
}

// SetChannelName renames a room.
func SetChannelName(id, name string) error {
	_, err := _db.Exec(`UPDATE channels SET name = ? WHERE id = ?`, name, id)
	return err
}

// SetChannelKey replaces a room's stream key (regenerate-on-demand). The
// default channel's keys live in the global list, not here.
func SetChannelKey(id, streamKey string) error {
	if id == DefaultChannelID {
		return errors.New("the default channel's keys are managed in the stream settings")
	}
	if streamKey == "" {
		return errors.New("a room needs a stream key")
	}
	_, err := _db.Exec(`UPDATE channels SET stream_key = ? WHERE id = ?`, streamKey, id)
	return err
}
