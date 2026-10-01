import json
import os
import re
import sqlite3
from typing import Dict, Any, Tuple, Optional

# Average characters per token across natural language, markdown, code, and JSON
CHARS_PER_TOKEN = 3.8

def estimate_tokens(text: str) -> int:
    if not text:
        return 0
    return max(1, int(round(len(text) / CHARS_PER_TOKEN)))

def parse_step(raw_line: str) -> Optional[Dict[str, Any]]:
    try:
        data = json.loads(raw_line)
    except Exception:
        return None

    step_index = data.get("step_index", 0)
    source = data.get("source", "UNKNOWN")
    step_type = data.get("type", "UNKNOWN")
    status = data.get("status", "DONE")
    created_at = data.get("created_at", "")

    content = data.get("content", "") or ""
    thinking = data.get("thinking", "") or ""
    tool_calls = data.get("tool_calls", None)

    content_chars = len(content)
    thinking_chars = len(thinking)
    tool_chars = len(json.dumps(tool_calls)) if tool_calls else 0

    total_chars = content_chars + thinking_chars + tool_chars
    tokens = estimate_tokens("x" * total_chars)

    return {
        "step_index": step_index,
        "source": source,
        "type": step_type,
        "status": status,
        "created_at": created_at,
        "content_chars": content_chars,
        "thinking_chars": thinking_chars,
        "tool_chars": tool_chars,
        "est_tokens": tokens,
    }

def get_conversation_title(conv_id: str, base_antigravity_dir: str) -> Optional[str]:
    # Check conversation_summaries.db first
    sum_db = os.path.join(base_antigravity_dir, "conversation_summaries.db")
    if os.path.exists(sum_db):
        try:
            conn = sqlite3.connect(sum_db)
            row = conn.execute("SELECT title FROM conversation_summaries WHERE conversation_id = ?", (conv_id,)).fetchone()
            conn.close()
            if row and row[0]:
                cleaned = re.sub(r'^\[[\d\.]+[kM]?\s*(tokens)?\]\s*', '', row[0], flags=re.IGNORECASE).strip()
                if cleaned:
                    return cleaned
        except Exception:
            pass

    # Fallback to annotations pbtxt
    pbtxt_path = os.path.join(base_antigravity_dir, "annotations", f"{conv_id}.pbtxt")
    if os.path.exists(pbtxt_path):
        try:
            with open(pbtxt_path, "r", encoding="utf-8") as f:
                content = f.read()
                m = re.search(r'title\s*:\s*"([^"]+)"', content)
                if m:
                    cleaned = re.sub(r'^\[[\d\.]+[kM]?\s*(tokens)?\]\s*', '', m.group(1), flags=re.IGNORECASE).strip()
                    if cleaned:
                        return cleaned
        except Exception:
            pass
    return None

def update_conversation_title_with_tokens(conv_id: str, base_antigravity_dir: str, est_total_tokens: int) -> bool:
    if est_total_tokens >= 1_000_000:
        token_tag = f"[{est_total_tokens/1_000_000:.1f}M]"
    elif est_total_tokens >= 1_000:
        token_tag = f"[{int(round(est_total_tokens/1_000))}k]"
    elif est_total_tokens > 0:
        token_tag = f"[{est_total_tokens}]"
    else:
        token_tag = "[0k]"

    updated = False

    # 1. Update in conversation_summaries.db (Primary data source for the sidebar list)
    sum_db = os.path.join(base_antigravity_dir, "conversation_summaries.db")
    if os.path.exists(sum_db):
        try:
            conn = sqlite3.connect(sum_db)
            row = conn.execute("SELECT title FROM conversation_summaries WHERE conversation_id = ?", (conv_id,)).fetchone()
            if row and row[0]:
                orig_title = row[0]
                cleaned = re.sub(r'^\[[\d\.]+[kM]?\s*(tokens)?\]\s*', '', orig_title, flags=re.IGNORECASE).strip()
                new_title = f"{token_tag} {cleaned}"
                if new_title != orig_title:
                    conn.execute("UPDATE conversation_summaries SET title = ? WHERE conversation_id = ?", (new_title, conv_id))
                    conn.commit()
                    updated = True
            conn.close()
        except Exception:
            pass

    # 2. Update in annotations/*.pbtxt
    pbtxt_path = os.path.join(base_antigravity_dir, "annotations", f"{conv_id}.pbtxt")
    if os.path.exists(pbtxt_path):
        try:
            with open(pbtxt_path, "r", encoding="utf-8") as f:
                content = f.read()

            m = re.search(r'title\s*:\s*"([^"]+)"', content)
            if m:
                orig_title = m.group(1)
                cleaned_title = re.sub(r'^\[[\d\.]+[kM]?\s*(tokens)?\]\s*', '', orig_title, flags=re.IGNORECASE).strip()
                new_title = f"{token_tag} {cleaned_title}"
                if new_title != orig_title:
                    updated_content = content[:m.start(1)] + new_title + content[m.end(1):]
                    with open(pbtxt_path, "w", encoding="utf-8") as f:
                        f.write(updated_content)
                    updated = True
        except Exception:
            pass

    return updated
