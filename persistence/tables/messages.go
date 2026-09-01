package tables

import (
	"database/sql"

	"streamingestarr/utils"
)

// CreateMessagesTable will create the chat messages table if needed.
func CreateMessagesTable(db *sql.DB) {
	createTableSQL := `CREATE TABLE IF NOT EXISTS messages (
		"id" string NOT NULL,
		"user_id" TEXT,
		"body" TEXT,
		"eventType" TEXT,
		"hidden_at" DATETIME,
		"timestamp" DATETIME,
		"title" TEXT,
		"subtitle" TEXT,
		"image" TEXT,
		"link" TEXT,
		PRIMARY KEY (id)
	);`
	utils.MustExec(createTableSQL, db)

	// Migration: chat messages belong to a room. '' means "all rooms" —
	// global admin/system messages every room's history includes.
	var hasChannel bool
	rows, err := db.Query(`SELECT name FROM pragma_table_info('messages')`)
	if err == nil {
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err == nil && name == "channel" {
				hasChannel = true
			}
		}
		rows.Close()
	}
	if !hasChannel {
		utils.MustExec(`ALTER TABLE messages ADD COLUMN channel TEXT NOT NULL DEFAULT 'main'`, db)
	}

	// Create indexes
	utils.MustExec(`CREATE INDEX IF NOT EXISTS user_id_hidden_at_timestamp ON messages (id, user_id, hidden_at, timestamp);`, db)
	utils.MustExec(`CREATE INDEX IF NOT EXISTS idx_id ON messages (id);`, db)
	utils.MustExec(`CREATE INDEX IF NOT EXISTS idx_user_id ON messages (user_id);`, db)
	utils.MustExec(`CREATE INDEX IF NOT EXISTS idx_hidden_at ON messages (hidden_at);`, db)
	utils.MustExec(`CREATE INDEX IF NOT EXISTS idx_timestamp ON messages (timestamp);`, db)
	utils.MustExec(`CREATE INDEX IF NOT EXISTS idx_messages_hidden_at_timestamp on messages(hidden_at, timestamp);`, db)
}
