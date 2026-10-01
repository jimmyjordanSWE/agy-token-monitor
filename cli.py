#!/usr/bin/env python3
import sys
import os
import argparse
import sqlite3

DB_PATH = os.path.expanduser("~/.gemini/antigravity/token_metrics.db")

def format_tokens(n: int) -> str:
    if n >= 1_000_000:
        return f"{n/1_000_000:.2f}M"
    elif n >= 1_000:
        return f"{n/1_000:.1f}k"
    return str(n)

def get_current_conv_id() -> str:
    pbtxt_dir = os.path.expanduser("~/.gemini/antigravity/annotations")
    best_id = "c06f2bd3-4f84-43af-9cb6-e4ca0968bd8c"
    best_time = 0
    if os.path.exists(pbtxt_dir):
        for f in os.listdir(pbtxt_dir):
            if f.endswith(".pbtxt"):
                full_path = os.path.join(pbtxt_dir, f)
                mtime = os.path.getmtime(full_path)
                if mtime > best_time:
                    best_time = mtime
                    best_id = f[:-6]
    return best_id

def show_projects():
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    rows = conn.execute("""
        SELECT 
            COALESCE(project_name, 'Unknown') as project,
            COUNT(*) as conv_count,
            SUM(total_steps) as total_steps,
            SUM(est_total_tokens) as total_tokens,
            SUM(est_user_tokens) as user_tokens,
            SUM(est_model_tokens) as model_tokens
        FROM conversation_summary
        GROUP BY project
        ORDER BY total_tokens DESC
    """).fetchall()

    print("=" * 85)
    print("📁 AGY TOKEN USAGE BY PROJECT / WORKSPACE")
    print("=" * 85)
    print(f"{'Project / Workspace':<30} | {'Convs':<6} | {'Steps':<7} | {'Total':<9} | {'User':<8} | {'Model':<8}")
    print("-" * 85)
    for r in rows:
        print(f"{r['project']:<30} | {r['conv_count']:<6} | {r['total_steps']:<7} | {format_tokens(r['total_tokens']):<9} | {format_tokens(r['user_tokens']):<8} | {format_tokens(r['model_tokens']):<8}")
    print("=" * 85)

def show_summary(conv_id: str):
    if not os.path.exists(DB_PATH):
        print("Database not found. Run daemon first.")
        return

    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    row = conn.execute("SELECT * FROM conversation_summary WHERE conversation_id = ?", (conv_id,)).fetchone()
    
    if not row:
        print(f"No metrics recorded for conversation: {conv_id}")
        return

    print("=" * 60)
    print(f"📊 AGY TOKEN USAGE REPORT")
    print("=" * 60)
    print(f"Conversation:  {row['title']}")
    print(f"Project:       {row['project_name'] or 'Unknown'}")
    print(f"ID:            {row['conversation_id']}")
    print(f"Total Steps:   {row['total_steps']}")
    print(f"Total Chars:   {row['total_chars']:,}")
    print("-" * 60)
    print(f"Estimated Total Tokens:    {format_tokens(row['est_total_tokens'])} ({row['est_total_tokens']:,})")
    print(f"  ├─ User Prompt Tokens:   {format_tokens(row['est_user_tokens'])} ({row['est_user_tokens']:,})")
    print(f"  ├─ Model Output Tokens:  {format_tokens(row['est_model_tokens'])} ({row['est_model_tokens']:,})")
    print(f"  ├─ Model Thinking:       {format_tokens(row['est_thinking_tokens'])} ({row['est_thinking_tokens']:,})")
    print(f"  └─ Tool Payload Tokens:  {format_tokens(row['est_tool_tokens'])} ({row['est_tool_tokens']:,})")
    print("=" * 60)

    steps = conn.execute("""
        SELECT step_index, source, type, est_tokens, created_at 
        FROM message_steps 
        WHERE conversation_id = ? 
        ORDER BY step_index DESC LIMIT 5
    """, (conv_id,)).fetchall()

    if steps:
        print("\nRecent Steps:")
        for s in reversed(steps):
            print(f"  Step #{s['step_index']:<4} | {s['source']:<13} | {s['type']:<16} | {format_tokens(s['est_tokens'])} tokens")
    print()

def show_top(limit: int = 10, project: str = None):
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    query = """
        SELECT conversation_id, title, COALESCE(project_name, 'Unknown') as project_name, total_steps, est_total_tokens, est_user_tokens, est_model_tokens 
        FROM conversation_summary 
    """
    params = []
    if project:
        query += " WHERE project_name LIKE ? "
        params.append(f"%{project}%")
    query += " ORDER BY est_total_tokens DESC LIMIT ?"
    params.append(limit)

    rows = conn.execute(query, tuple(params)).fetchall()

    print("=" * 95)
    print(f"🏆 TOP {limit} CONVERSATIONS BY TOKEN CONSUMPTION" + (f" (Project: {project})" if project else ""))
    print("=" * 95)
    print(f"{'Title':<35} | {'Project':<20} | {'Steps':<6} | {'Total':<8} | {'User':<7} | {'Model':<7}")
    print("-" * 95)
    for r in rows:
        title = (r['title'] or "Untitled")[:33]
        proj = (r['project_name'] or "Unknown")[:18]
        print(f"{title:<35} | {proj:<20} | {r['total_steps']:<6} | {format_tokens(r['est_total_tokens']):<8} | {format_tokens(r['est_user_tokens']):<7} | {format_tokens(r['est_model_tokens']):<7}")
    print("=" * 95)

def export_data(fmt: str, out_path: str = None, conv_id: str = None):
    if not os.path.exists(DB_PATH):
        print("Database not found. Run daemon first.", file=sys.stderr)
        return

    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row

    if conv_id:
        conv_row = conn.execute("SELECT * FROM conversation_summary WHERE conversation_id = ?", (conv_id,)).fetchone()
        if not conv_row:
            print(f"Conversation not found: {conv_id}", file=sys.stderr)
            return
        step_rows = conn.execute("SELECT * FROM message_steps WHERE conversation_id = ? ORDER BY step_index ASC", (conv_id,)).fetchall()
        data = {
            "summary": dict(conv_row),
            "steps": [dict(s) for s in step_rows]
        }
    else:
        conv_rows = conn.execute("SELECT * FROM conversation_summary ORDER BY est_total_tokens DESC").fetchall()
        data = [dict(r) for r in conv_rows]

    output_str = ""
    if fmt == "json":
        import json
        output_str = json.dumps(data, indent=2, ensure_ascii=False)
    elif fmt == "csv":
        import csv
        import io
        buf = io.StringIO()
        if conv_id:
            steps = data.get("steps", [])
            if steps:
                writer = csv.DictWriter(buf, fieldnames=steps[0].keys())
                writer.writeheader()
                writer.writerows(steps)
            else:
                buf.write("No steps recorded\n")
        else:
            if data:
                writer = csv.DictWriter(buf, fieldnames=data[0].keys())
                writer.writeheader()
                writer.writerows(data)
        output_str = buf.getvalue()

    if out_path:
        with open(out_path, "w", encoding="utf-8") as f:
            f.write(output_str)
        print(f"Exported metrics to: {out_path}")
    else:
        print(output_str)

def main():
    parser = argparse.ArgumentParser(description="AGY Token Usage Inspector & Benchmarker")
    parser.add_argument("-c", "--conversation", help="Specific conversation ID (defaults to current active)")
    parser.add_argument("-p", "--projects", action="store_true", help="List token usage aggregated by project")
    parser.add_argument("-t", "--top", type=int, nargs="?", const=10, help="Show top conversations by token usage")
    parser.add_argument("--filter-project", type=str, help="Filter top conversations by project name")
    parser.add_argument("--export", choices=["json", "csv"], help="Export conversation token metrics (json or csv)")
    parser.add_argument("--out", type=str, help="Output file path for export (default: stdout)")
    parser.add_argument("-q", "--query", type=str, help="Run custom SQL query on token_metrics.db")

    args = parser.parse_args()

    if args.export:
        export_data(args.export, args.out, args.conversation)
        return

    if args.query:
        conn = sqlite3.connect(DB_PATH)
        cur = conn.cursor()
        for r in cur.execute(args.query):
            print(r)
        return

    if args.projects:
        show_projects()
    elif args.top is not None:
        show_top(args.top, args.filter_project)
    else:
        conv_id = args.conversation or get_current_conv_id()
        show_summary(conv_id)

if __name__ == "__main__":
    main()
