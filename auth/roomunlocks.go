package auth

import (
	"database/sql"
	"time"

	log "github.com/sirupsen/logrus"
)

// Room unlocks: a session that has entered a room's own password once is
// not asked again. The unlock belongs to the session — it dies with it,
// and a room's password change clears every unlock for that room.

func setupRoomUnlocks(db *sql.DB) {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS room_unlocks (
		"token_hash" TEXT NOT NULL,
		"channel_id" TEXT NOT NULL,
		"created_at" INTEGER NOT NULL,
		PRIMARY KEY (token_hash, channel_id)
	)`); err != nil {
		log.Fatalln("unable to create room_unlocks table:", err)
	}
}

// MarkRoomUnlocked remembers that this session passed the room's password.
func MarkRoomUnlocked(token, channelID string) {
	if token == "" || _db == nil {
		return
	}
	_, _ = _db.Exec(`INSERT OR REPLACE INTO room_unlocks(token_hash, channel_id, created_at) VALUES (?, ?, ?)`,
		hashToken(token), channelID, time.Now().Unix())
}

// RoomUnlocked reports whether this session passed the room's password.
func RoomUnlocked(token, channelID string) bool {
	if token == "" || _db == nil {
		return false
	}
	var n int
	row := _db.QueryRow(`SELECT COUNT(*) FROM room_unlocks WHERE token_hash = ? AND channel_id = ?`, hashToken(token), channelID)
	return row.Scan(&n) == nil && n > 0
}

// ClearRoomUnlocks forgets every unlock of a room — its password changed
// or the room is gone.
func ClearRoomUnlocks(channelID string) {
	if _db == nil {
		return
	}
	_, _ = _db.Exec(`DELETE FROM room_unlocks WHERE channel_id = ?`, channelID)
}

// pruneOrphanUnlocks drops unlocks whose session no longer exists.
func pruneOrphanUnlocks() {
	if _db == nil {
		return
	}
	_, _ = _db.Exec(`DELETE FROM room_unlocks WHERE token_hash NOT IN (SELECT token_hash FROM auth_sessions)`)
}
