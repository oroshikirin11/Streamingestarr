// Package channelrepository stores the channel (theater) list. The default
// channel always exists; additional rooms are plain data rows, each carrying
// its own stream key — the key is how an inbound stream picks its room, so
// no extra ports ever open.
package channelrepository

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"streamingestarr/models"
	"streamingestarr/persistence/configrepository"
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
	// Per-room broadcast configuration; zero values inherit the server
	// defaults, so existing rooms behave exactly as before.
	for col, ddl := range map[string]string{
		"title":           `ALTER TABLE channels ADD COLUMN title TEXT NOT NULL DEFAULT ''`,
		"welcome_message": `ALTER TABLE channels ADD COLUMN welcome_message TEXT NOT NULL DEFAULT ''`,
		"latency_level":   `ALTER TABLE channels ADD COLUMN latency_level INTEGER NOT NULL DEFAULT -1`,
		"segment_format":  `ALTER TABLE channels ADD COLUMN segment_format TEXT NOT NULL DEFAULT ''`,
		"output_variants": `ALTER TABLE channels ADD COLUMN output_variants TEXT NOT NULL DEFAULT ''`,
		"passphrase":      `ALTER TABLE channels ADD COLUMN passphrase TEXT NOT NULL DEFAULT ''`,
		// Viewer pause votes, on unless an admin switches a room off.
		"pause_vote_enabled": `ALTER TABLE channels ADD COLUMN pause_vote_enabled INTEGER NOT NULL DEFAULT 1`,
		// Room mode, relay links and the room's own password (Sep 2026).
		"mode":            `ALTER TABLE channels ADD COLUMN mode TEXT NOT NULL DEFAULT ''`,
		"relay_protocols": `ALTER TABLE channels ADD COLUMN relay_protocols TEXT NOT NULL DEFAULT ''`,
		"relay_token":     `ALTER TABLE channels ADD COLUMN relay_token TEXT NOT NULL DEFAULT ''`,
		"room_password":   `ALTER TABLE channels ADD COLUMN room_password TEXT NOT NULL DEFAULT ''`,
		"lock_theater":    `ALTER TABLE channels ADD COLUMN lock_theater INTEGER NOT NULL DEFAULT 0`,
		"lock_relay":      `ALTER TABLE channels ADD COLUMN lock_relay INTEGER NOT NULL DEFAULT 0`,
	} {
		if !columnExists(db, "channels", col) {
			if _, err := db.Exec(ddl); err != nil {
				log.Fatalln("unable to add", col, "column to channels:", err)
			}
		}
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO channels(id, name) VALUES(?, ?)`,
		DefaultChannelID, "Main Theater"); err != nil {
		log.Fatalln("unable to seed default channel:", err)
	}

	// One-time migration: the pre-rooms GLOBAL stream title and welcome
	// message move into the main room, which is now the only place they
	// live — the Settings/Chat sections stopped editing the global copies.
	// Every room carries a relay token from the start, so switching a room
	// to relay never has to mint one on the fly.
	backfillRelayTokens(db)

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS channel_meta (
		"key" TEXT NOT NULL PRIMARY KEY,
		"value" TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		log.Fatalln("unable to create channel_meta table:", err)
	}
	var done string
	_ = db.QueryRow(`SELECT value FROM channel_meta WHERE key = 'identity_migrated'`).Scan(&done)
	if done == "" {
		cfg := configrepository.Get()
		if t := cfg.GetStreamTitle(); t != "" {
			_, _ = db.Exec(`UPDATE channels SET title = ? WHERE id = ? AND title = ''`, t, DefaultChannelID)
		}
		if wm := cfg.GetServerWelcomeMessage(); wm != "" {
			_, _ = db.Exec(`UPDATE channels SET welcome_message = ? WHERE id = ? AND welcome_message = ''`, wm, DefaultChannelID)
		}
		_, _ = db.Exec(`INSERT OR REPLACE INTO channel_meta(key, value) VALUES('identity_migrated', 'yes')`)
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
	var variantsJSON string
	var protocols string
	row := _db.QueryRow("SELECT id, name, title, welcome_message, latency_level, segment_format, output_variants, passphrase, pause_vote_enabled, mode, relay_protocols, relay_token, room_password, lock_theater, lock_relay FROM channels WHERE id = ?", id)
	if err := row.Scan(&c.ID, &c.Name, &c.Title, &c.WelcomeMessage, &c.LatencyLevel, &c.SegmentFormat, &variantsJSON, &c.Passphrase, &c.PauseVoteEnabled, &c.Mode, &protocols, &c.RelayToken, &c.RoomPasswordHash, &c.LockTheater, &c.LockRelay); err != nil {
		return nil
	}
	if variantsJSON != "" {
		_ = json.Unmarshal([]byte(variantsJSON), &c.OutputVariants)
	}
	if protocols != "" {
		c.RelayProtocols = strings.Split(protocols, ",")
	}
	// Locks without a password guard nothing.
	if c.RoomPasswordHash == "" {
		c.LockTheater, c.LockRelay = false, false
	}
	c.Keys = ListChannelKeys(id)
	return &c
}

// ListChannels returns all channels with their keys, oldest first.
func ListChannels() []models.Channel {
	// One reader for a room: GetChannel. The list is the ids in order,
	// so a column added there is never missed here.
	rows, err := _db.Query("SELECT id FROM channels ORDER BY created_at")
	if err != nil {
		return nil
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	var channels []models.Channel
	for _, id := range ids {
		if c := GetChannel(id); c != nil {
			channels = append(channels, *c)
		}
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
	// A relay token from the first day: a room switched to relay later must
	// have links at once, not after the next restart's backfill.
	res, err := _db.Exec(`INSERT OR IGNORE INTO channels(id, name, relay_token) VALUES(?, ?, ?)`, id, name, NewRelayToken())
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

// SetChannelConfig stores a room's broadcast configuration. Zero values
// ("" / -1 / empty variants) mean "inherit the server defaults".
func SetChannelConfig(id string, c models.Channel) error {
	variantsJSON := ""
	if len(c.OutputVariants) > 0 {
		b, err := json.Marshal(c.OutputVariants)
		if err != nil {
			return err
		}
		variantsJSON = string(b)
	}
	if c.LatencyLevel < -1 || c.LatencyLevel > 4 {
		c.LatencyLevel = -1
	}
	switch c.SegmentFormat {
	case "", "auto", "ts", "fmp4":
	default:
		return errors.New("segment format must be auto, ts or fmp4 (or empty to inherit)")
	}
	_, err := _db.Exec(`UPDATE channels SET title = ?, welcome_message = ?, latency_level = ?, segment_format = ?, output_variants = ? WHERE id = ?`,
		c.Title, c.WelcomeMessage, c.LatencyLevel, c.SegmentFormat, variantsJSON, id)
	return err
}

// The effective-config lens: what a room ACTUALLY broadcasts with — its own
// setting when one is stored, the server default otherwise. Every consumer
// that used to read the global config for a live broadcast reads these.

// SetChannelPassphrase stores a room's own ingest passphrase; "" clears it
// so the global one applies again.
func SetChannelPassphrase(id, passphrase string) error {
	_, err := _db.Exec(`UPDATE channels SET passphrase = ? WHERE id = ?`, passphrase, id)
	return err
}

// SetChannelPauseVote flips the room's "viewers may vote to pause" switch.
func SetChannelPauseVote(id string, enabled bool) error {
	_, err := _db.Exec(`UPDATE channels SET pause_vote_enabled = ? WHERE id = ?`, enabled, id)
	return err
}

// PassphraseForKey returns the own passphrase of the room a stream key
// opens — "" when that room has none. A key from the global list opens
// the default room.
func PassphraseForKey(key string) string {
	id := GetChannelIDForKey(key)
	if id == "" {
		id = DefaultChannelID
	}
	if c := GetChannel(id); c != nil {
		return c.Passphrase
	}
	return ""
}

// GetEffectiveStreamTitle returns the room's title. Titles are per-room
// only — the old global title migrated into the main room at Setup.
func GetEffectiveStreamTitle(channelID string) string {
	if c := GetChannel(channelID); c != nil {
		return c.Title
	}
	return ""
}

// GetEffectiveWelcomeMessage returns the room's chat welcome message —
// per-room only, same migration story as the title.
func GetEffectiveWelcomeMessage(channelID string) string {
	if c := GetChannel(channelID); c != nil {
		return c.WelcomeMessage
	}
	return ""
}

// SetChannelTitle updates just a room's stream title — the surface the
// legacy streamtitle API endpoints write through now.
func SetChannelTitle(id, title string) error {
	_, err := _db.Exec(`UPDATE channels SET title = ? WHERE id = ?`, title, id)
	return err
}

// GetEffectiveLatencyLevel returns the room's latency level, falling back
// to the global one.
func GetEffectiveLatencyLevel(channelID string) models.LatencyLevel {
	if c := GetChannel(channelID); c != nil && c.LatencyLevel >= 0 {
		return models.GetLatencyLevel(c.LatencyLevel)
	}
	return configrepository.Get().GetStreamLatencyLevel()
}

// GetEffectiveSegmentFormat returns the room's stored segment format
// ("auto"/"ts"/"fmp4"), falling back to the global one.
func GetEffectiveSegmentFormat(channelID string) string {
	if c := GetChannel(channelID); c != nil && c.SegmentFormat != "" {
		return c.SegmentFormat
	}
	return configrepository.Get().GetVideoSegmentFormat()
}

// GetEffectiveOutputVariants returns the room's output ladder, falling back
// to the global one.
func GetEffectiveOutputVariants(channelID string) []models.StreamOutputVariant {
	if c := GetChannel(channelID); c != nil && len(c.OutputVariants) > 0 {
		return c.OutputVariants
	}
	return configrepository.Get().GetStreamOutputVariants()
}

// --- Room mode, relay links and the room password ---------------------

// NewRelayToken mints the secret that rides in every relay link.
func NewRelayToken() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// backfillRelayTokens gives every token-less room one.
func backfillRelayTokens(db *sql.DB) {
	rows, err := db.Query(`SELECT id FROM channels WHERE relay_token = ''`)
	if err != nil {
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	_ = rows.Close()
	for _, id := range ids {
		if _, err := db.Exec(`UPDATE channels SET relay_token = ? WHERE id = ?`, NewRelayToken(), id); err != nil {
			log.Errorln("unable to set a relay token for room", id, err)
		}
	}
}

// SetChannelMode stores the room mode and the relay link kinds.
func SetChannelMode(id, mode string, protocols []string) error {
	switch mode {
	case models.RoomModeTheater, models.RoomModeRelay, models.RoomModeBoth:
	default:
		return errors.New("mode must be theater, relay or both")
	}
	clean := models.Channel{RelayProtocols: protocols}.EffectiveRelayProtocols()
	_, err := _db.Exec(`UPDATE channels SET mode = ?, relay_protocols = ? WHERE id = ?`, mode, strings.Join(clean, ","), id)
	return err
}

// RotateRelayToken replaces a room's relay token and returns the new one.
func RotateRelayToken(id string) (string, error) {
	token := NewRelayToken()
	_, err := _db.Exec(`UPDATE channels SET relay_token = ? WHERE id = ?`, token, id)
	return token, err
}

// SetChannelPassword stores the room password hash; "" clears it and
// drops both locks with it.
func SetChannelPassword(id, hash string) error {
	if hash == "" {
		_, err := _db.Exec(`UPDATE channels SET room_password = '', lock_theater = 0, lock_relay = 0 WHERE id = ?`, id)
		return err
	}
	_, err := _db.Exec(`UPDATE channels SET room_password = ? WHERE id = ?`, hash, id)
	return err
}

// SetChannelLocks says what the room password guards.
func SetChannelLocks(id string, theater, relay bool) error {
	_, err := _db.Exec(`UPDATE channels SET lock_theater = ?, lock_relay = ? WHERE id = ?`, theater, relay, id)
	return err
}
