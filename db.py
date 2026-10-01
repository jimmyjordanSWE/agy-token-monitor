import sqlite3
import os
from typing import Optional, Dict, Any, List

DB_PATH = os.path.expanduser("~/.gemini/antigravity/token_metrics.db")

def get_connection(db_path: str = DB_PATH) -> sqlite3.Connection:
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    return conn

def init_db(db_path: str = DB_PATH) -> None:
    os.makedirs(os.path.dirname(db_path), exist_ok=True)
    with get_connection(db_path) as conn:
        conn.execute("""
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
                last_updated TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            )
        """)
        conn.execute("""
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
            )
        """)
        conn.execute("CREATE INDEX IF NOT EXISTS idx_step_conv ON message_steps(conversation_id);")
        try:
            conn.execute("ALTER TABLE conversation_summary ADD COLUMN last_synced_tokens INTEGER DEFAULT -1;")
        except Exception:
            pass
        conn.commit()

if __name__ == "__main__":
    init_db()
    print("Database initialized successfully at", DB_PATH)
