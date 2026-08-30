// Package channelrepository stores the channel (theater) list. Single-row
// today by design; the schema and lookups are what keep multi-channel a
// data change instead of a refactor.
package channelrepository

import (
	"database/sql"
	"regexp"

	log "github.com/sirupsen/logrus"

	"streamingestarr/models"
)

// DefaultChannelID is the channel every fresh install gets, and the one
// legacy unscoped URLs (/hls/stream.m3u8) resolve to.
const DefaultChannelID = "main"

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
	if _, err := db.Exec(`INSERT OR IGNORE INTO channels(id, name) VALUES(?, ?)`,
		DefaultChannelID, "Main Theater"); err != nil {
		log.Fatalln("unable to seed default channel:", err)
	}
}

// GetChannel returns a channel by ID, or nil if it does not exist.
func GetChannel(id string) *models.Channel {
	var c models.Channel
	row := _db.QueryRow("SELECT id, name FROM channels WHERE id = ?", id)
	if err := row.Scan(&c.ID, &c.Name); err != nil {
		return nil
	}
	return &c
}

// ListChannels returns all channels, oldest first.
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
	return channels
}
