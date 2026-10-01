package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

// ConversationSummary models aggregated token stats for a conversation.
type ConversationSummary struct {
	ConversationID    string    `json:"conversation_id"`
	Title             string    `json:"title"`
	ProjectName       string    `json:"project_name"`
	TotalSteps        int       `json:"total_steps"`
	TotalChars        int64     `json:"total_chars"`
	EstTotalTokens    int64     `json:"est_total_tokens"`
	EstUserTokens     int64     `json:"est_user_tokens"`
	EstModelTokens    int64     `json:"est_model_tokens"`
	EstToolTokens     int64     `json:"est_tool_tokens"`
	EstThinkingTokens int64     `json:"est_thinking_tokens"`
	LastUpdated       time.Time `json:"last_updated"`
	LastSyncedTokens  int64     `json:"last_synced_tokens"`
}

// MessageStep models an individual step from transcript_full.jsonl.
type MessageStep struct {
	ID             int64  `json:"id"`
	ConversationID string `json:"conversation_id"`
	StepIndex      int    `json:"step_index"`
	Source         string `json:"source"`
	Type           string `json:"type"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	ContentChars   int    `json:"content_chars"`
	ThinkingChars  int    `json:"thinking_chars"`
	ToolChars      int    `json:"tool_chars"`
	EstTokens      int64  `json:"est_tokens"`
}

// ProjectSummary models aggregated stats per project.
type ProjectSummary struct {
	ProjectName    string `json:"project_name"`
	ConvCount      int    `json:"conv_count"`
	TotalSteps     int    `json:"total_steps"`
	EstTotalTokens int64  `json:"est_total_tokens"`
	EstUserTokens  int64  `json:"est_user_tokens"`
	EstModelTokens int64  `json:"est_model_tokens"`
}

// OpenDB opens a connection to SQLite with concurrency-friendly pragmas.
func OpenDB(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	// Limit idle connections to avoid descriptor exhaustion
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(1 * time.Hour)

	return db, nil
}

// InitDB initializes tables, indices, and migrations in SQLite.
func InitDB(dbPath string) (*sql.DB, error) {
	db, err := OpenDB(dbPath)
	if err != nil {
		return nil, err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS conversation_summary (
		conversation_id TEXT PRIMARY KEY,
		title TEXT,
		project_name TEXT,
		total_steps INTEGER DEFAULT 0,
		total_chars INTEGER DEFAULT 0,
		est_total_tokens INTEGER DEFAULT 0,
		est_user_tokens INTEGER DEFAULT 0,
		est_model_tokens INTEGER DEFAULT 0,
		est_tool_tokens INTEGER DEFAULT 0,
		est_thinking_tokens INTEGER DEFAULT 0,
		last_updated TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_synced_tokens INTEGER DEFAULT -1
	);

	CREATE TABLE IF NOT EXISTS message_steps (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		conversation_id TEXT,
		step_index INTEGER,
		source TEXT,
		type TEXT,
		status TEXT,
		created_at TEXT,
		content_chars INTEGER DEFAULT 0,
		thinking_chars INTEGER DEFAULT 0,
		tool_chars INTEGER DEFAULT 0,
		est_tokens INTEGER DEFAULT 0,
		UNIQUE(conversation_id, step_index)
	);

	CREATE INDEX IF NOT EXISTS idx_step_conv ON message_steps(conversation_id);

	CREATE TABLE IF NOT EXISTS system_config (
		key TEXT PRIMARY KEY,
		value TEXT
	);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to execute schema: %w", err)
	}

	// Graceful migration if column is missing from earlier schema versions
	_, _ = db.Exec("ALTER TABLE conversation_summary ADD COLUMN last_synced_tokens INTEGER DEFAULT -1;")

	return db, nil
}

// GetHandoffTarget retrieves the target handoff token threshold from system_config.
func GetHandoffTarget(db *sql.DB) int {
	var val string
	err := db.QueryRow("SELECT value FROM system_config WHERE key = 'handoff_target'").Scan(&val)
	if err != nil {
		return 150000
	}
	t, err := strconv.Atoi(val)
	if err != nil {
		return 150000
	}
	return t
}

// GetManualHandoffFlag checks if manual handoff was requested.
func GetManualHandoffFlag(db *sql.DB) bool {
	var val string
	err := db.QueryRow("SELECT value FROM system_config WHERE key = 'manual_handoff_flag'").Scan(&val)
	if err != nil {
		return false
	}
	return val == "1"
}

// SetSystemConfig sets a key-value pair in system_config table.
func SetSystemConfig(db *sql.DB, key, val string) error {
	_, err := db.Exec("INSERT OR REPLACE INTO system_config (key, value) VALUES (?, ?)", key, val)
	return err
}
