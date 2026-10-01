import os
import sys
import time
import glob
import sqlite3
from typing import Dict

from db import init_db, get_connection
from parser import parse_step, get_conversation_title, update_conversation_title_with_tokens

ANTIGRAVITY_DIR = os.path.expanduser("~/.gemini/antigravity")
BRAIN_DIR = os.path.join(ANTIGRAVITY_DIR, "brain")
RULES_DIR = os.path.join(ANTIGRAVITY_DIR, "rules")

file_offsets: Dict[str, int] = {}
last_title_update: Dict[str, float] = {}

HANDOFF_RULE_PATH = os.path.join(RULES_DIR, "context_handoff.md")

def get_handoff_target(conn: sqlite3.Connection) -> int:
    try:
        conn.execute("CREATE TABLE IF NOT EXISTS system_config (key TEXT PRIMARY KEY, value TEXT);")
        row = conn.execute("SELECT value FROM system_config WHERE key='handoff_target';").fetchone()
        if row and row[0]:
            return int(row[0])
    except Exception:
        pass
    return 150000

def check_handoff_injection_for_active_conv(conn: sqlite3.Connection):
    # Find the most recently updated conversation
    row = conn.execute("SELECT conversation_id, est_total_tokens FROM conversation_summary ORDER BY last_updated DESC LIMIT 1;").fetchone()
    if not row:
        return
    
    conv_id = row[0]
    total_tokens = row[1]
    target = get_handoff_target(conn)

    # Check if manual injection flag was set or threshold was crossed
    manual_flag = False
    try:
        flag_row = conn.execute("SELECT value FROM system_config WHERE key='manual_handoff_flag';").fetchone()
        if flag_row and flag_row[0] == '1':
            manual_flag = True
    except Exception:
        pass

    if manual_flag or (target > 0 and total_tokens >= target):
        os.makedirs(RULES_DIR, exist_ok=True)
        reason = "User requested immediate handoff." if manual_flag else f"Tokens ({total_tokens:,}) exceeded target ({target:,})."
        content = f"""---
description: Critical automatic context handoff directive
---

# ⚠️ CRITICAL CONTEXT LIMIT DIRECTIVE ({reason})
Notice: The current conversation has reached its context handoff limit.

## INSTRUCTIONS FOR ASSISTANT:
1. Complete your immediate sub-task cleanly. Do NOT initiate new large refactors or long investigative tangents.
2. Ensure working state is verified (git status, clean files).
3. Conclude your turn with a clear, structured **HANDOFF SUMMARY**:
   - **Status & Accomplishments**: What was just completed.
   - **Key Context / Files**: Specific files modified.
   - **Next Action**: The immediate next step to take.
   - **Continuation Prompt**: A copy-pasteable prompt for the user to provide when opening the new chat.
"""
        with open(HANDOFF_RULE_PATH, "w", encoding="utf-8") as f:
            f.write(content)
    else:
        # If tokens are low (new chat started), clear the rule
        if os.path.exists(HANDOFF_RULE_PATH):
            try:
                os.remove(HANDOFF_RULE_PATH)
            except Exception:
                pass

def detect_project(conv_id: str) -> str:
    t_file = os.path.join(BRAIN_DIR, conv_id, ".system_generated", "logs", "transcript.jsonl")
    if not os.path.exists(t_file):
        return "Unknown"
    
    cwds = []
    try:
        with open(t_file, "r", errors="ignore") as f:
            for _ in range(40):
                line = f.readline()
                if not line:
                    break
                import json
                try:
                    data = json.loads(line)
                    for tc in data.get("tool_calls", []):
                        args = tc.get("args", {})
                        if "Cwd" in args:
                            cwds.append(args["Cwd"])
                        if "TargetFile" in args:
                            cwds.append(args["TargetFile"])
                except Exception:
                    pass
    except Exception:
        pass
    
    joined = " ".join(cwds)
    if "data_mine" in joined:
        return "data_mine"
    elif "treeclimber" in joined or "tinrook" in joined:
        return "tinrook (treeclimber)"
    elif "nordiska-team1-1.worktrees" in joined or "pr-74" in joined:
        return "nordiska (pr-74 worktree)"
    elif "nordiska-team1-1" in joined:
        return "nordiska-team1-1"
    elif "nordiska" in joined:
        return "nordiska-team1"
    elif "/home/jimmy" in joined:
        return "system / home"
    return "general / other"

def process_transcript_file(conv_id: str, file_path: str, conn: sqlite3.Connection):
    if not os.path.exists(file_path):
        return

    last_offset = file_offsets.get(file_path, 0)
    current_size = os.path.getsize(file_path)

    if current_size < last_offset:
        last_offset = 0

    if current_size == last_offset:
        return

    with open(file_path, "r", encoding="utf-8", errors="replace") as f:
        f.seek(last_offset)
        lines = f.readlines()
        file_offsets[file_path] = f.tell()

    if not lines:
        return

    for line in lines:
        line = line.strip()
        if not line:
            continue
        step = parse_step(line)
        if not step:
            continue

        conn.execute("""
            INSERT INTO message_steps (
                conversation_id, step_index, source, type, status, created_at,
                content_chars, thinking_chars, tool_chars, est_tokens
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(conversation_id, step_index) DO UPDATE SET
                source=excluded.source,
                type=excluded.type,
                status=excluded.status,
                content_chars=excluded.content_chars,
                thinking_chars=excluded.thinking_chars,
                tool_chars=excluded.tool_chars,
                est_tokens=excluded.est_tokens
        """, (
            conv_id,
            step["step_index"],
            step["source"],
            step["type"],
            step["status"],
            step["created_at"],
            step["content_chars"],
            step["thinking_chars"],
            step["tool_chars"],
            step["est_tokens"]
        ))

    row = conn.execute("""
        SELECT 
            COUNT(*) as total_steps,
            SUM(content_chars + thinking_chars + tool_chars) as total_chars,
            SUM(est_tokens) as total_tokens,
            SUM(CASE WHEN source = 'USER_EXPLICIT' THEN est_tokens ELSE 0 END) as user_tokens,
            SUM(CASE WHEN source = 'MODEL' THEN est_tokens ELSE 0 END) as model_tokens,
            SUM(CASE WHEN tool_chars > 0 THEN est_tokens ELSE 0 END) as tool_tokens,
            SUM(CASE WHEN thinking_chars > 0 THEN est_tokens ELSE 0 END) as thinking_tokens
        FROM message_steps
        WHERE conversation_id = ?
    """, (conv_id,)).fetchone()

    total_steps = row["total_steps"] or 0
    total_chars = row["total_chars"] or 0
    total_tokens = row["total_tokens"] or 0
    user_tokens = row["user_tokens"] or 0
    model_tokens = row["model_tokens"] or 0
    tool_tokens = row["tool_tokens"] or 0
    thinking_tokens = row["thinking_tokens"] or 0

    title = get_conversation_title(conv_id, ANTIGRAVITY_DIR) or "Untitled"
    project_name = detect_project(conv_id)

    conn.execute("""
        INSERT INTO conversation_summary (
            conversation_id, title, project_name, total_steps, total_chars,
            est_total_tokens, est_user_tokens, est_model_tokens,
            est_tool_tokens, est_thinking_tokens, last_updated
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
        ON CONFLICT(conversation_id) DO UPDATE SET
            title=excluded.title,
            project_name=excluded.project_name,
            total_steps=excluded.total_steps,
            total_chars=excluded.total_chars,
            est_total_tokens=excluded.est_total_tokens,
            est_user_tokens=excluded.est_user_tokens,
            est_model_tokens=excluded.est_model_tokens,
            est_tool_tokens=excluded.est_tool_tokens,
            est_thinking_tokens=excluded.est_thinking_tokens,
            last_updated=CURRENT_TIMESTAMP
    """, (
        conv_id, title, project_name, total_steps, total_chars,
        total_tokens, user_tokens, model_tokens,
        tool_tokens, thinking_tokens
    ))
    conn.commit()
    mark_dirty(conv_id)

dirty_conversations = set()
last_flushed_time: Dict[str, float] = {}
known_transcripts: Dict[str, str] = {}
last_brain_mtime: float = 0.0

def mark_dirty(conv_id: str):
    dirty_conversations.add(conv_id)

def flush_dirty_queue(conn: sqlite3.Connection, debounce_sec: float = 3.0, force: bool = False):
    if not dirty_conversations:
        return
    now = time.time()
    flushed = []
    for conv_id in list(dirty_conversations):
        if not force and (now - last_flushed_time.get(conv_id, 0.0) < debounce_sec):
            continue
        
        row = conn.execute("SELECT est_total_tokens, last_synced_tokens FROM conversation_summary WHERE conversation_id = ?", (conv_id,)).fetchone()
        if not row:
            flushed.append(conv_id)
            continue
        
        tokens = row[0] or 0
        last_synced = row[1]

        if tokens != last_synced:
            update_conversation_title_with_tokens(conv_id, ANTIGRAVITY_DIR, tokens)
            conn.execute("UPDATE conversation_summary SET last_synced_tokens = ? WHERE conversation_id = ?", (tokens, conv_id))
            conn.commit()

        last_flushed_time[conv_id] = now
        flushed.append(conv_id)

    for cid in flushed:
        dirty_conversations.discard(cid)

def init_dirty_state(conn: sqlite3.Connection):
    """Detect and flush any conversations whose tokens were never synced or changed while daemon was down."""
    try:
        rows = conn.execute("""
            SELECT conversation_id, est_total_tokens 
            FROM conversation_summary 
            WHERE last_synced_tokens IS NULL OR last_synced_tokens != est_total_tokens
        """).fetchall()
        for row in rows:
            conv_id = row[0]
            mark_dirty(conv_id)
        flush_dirty_queue(conn, force=True)
    except Exception:
        pass

def get_transcript_files() -> Dict[str, str]:
    """Smart transcript discovery: only re-scans brain/ directory if its mtime changed."""
    global last_brain_mtime, known_transcripts
    if not os.path.exists(BRAIN_DIR):
        return {}
    try:
        current_mtime = os.stat(BRAIN_DIR).st_mtime
    except Exception:
        current_mtime = 0.0

    if current_mtime != last_brain_mtime or not known_transcripts:
        new_map = {}
        pattern = os.path.join(BRAIN_DIR, "*", ".system_generated", "logs", "transcript_full.jsonl")
        for f in glob.glob(pattern):
            parts = f.split(os.sep)
            try:
                b_idx = parts.index("brain")
                c_id = parts[b_idx + 1]
                new_map[c_id] = f
            except Exception:
                pass
        known_transcripts = new_map
        last_brain_mtime = current_mtime

    return known_transcripts

def run_sync_once():
    init_db()
    conn = get_connection()
    transcripts = get_transcript_files()
    for conv_id, file_path in transcripts.items():
        process_transcript_file(conv_id, file_path, conn)
    flush_dirty_queue(conn, force=True)
    check_handoff_injection_for_active_conv(conn)
    conn.close()

def main():
    init_db()
    conn = get_connection()
    init_dirty_state(conn)
    conn.close()
    print("AGY Token Monitor Daemon running (event-driven dirty queue)...")

    while True:
        try:
            conn = get_connection()
            transcripts = get_transcript_files()
            for conv_id, file_path in transcripts.items():
                process_transcript_file(conv_id, file_path, conn)

            flush_dirty_queue(conn, debounce_sec=3.0)
            check_handoff_injection_for_active_conv(conn)
            conn.close()
        except Exception as e:
            print(f"Error in sync loop: {e}", file=sys.stderr)
        time.sleep(1.0)

if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--once":
        run_sync_once()
    else:
        main()
