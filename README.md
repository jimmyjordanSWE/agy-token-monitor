# AGY Token Monitor & Handoff Extension

A lightweight, non-intrusive token tracker, compression monitor, and automatic context handoff system for Google Antigravity in VS Code on Linux / WSL.

## Overview

As long-running autonomous agent sessions proceed, token accumulation causes:
1. **Attention dilution**: Older constraints get deprioritized as tool outputs fill the context window.
2. **Internal compression cliffs**: At periodic milestones (~170k, ~340k tokens), the agent runtime triggers an internal compression pass, summarizing past turns and invalidating prefix prompt caches.

`agy-token-monitor` provides:
- **Real-Time Context Tracking**: Live token consumption, compression cycle counts (`C0`, `C1`, `C2`), and exact headroom remaining before the next internal compression event.
- **Single-Field Status Bar**: Clean display: `AGY: 154.2k handoff 150k` with color coding (Green / Yellow / Red).
- **Automated / Manual Handoff**: Configurable targets or one-click instant handoff directives injected into the agent's instructions, ensuring clean summaries before context compression occurs.
- **Historical Benchmarking**: SQLite database and CLI (`agy-tokens`) to analyze token distribution across tasks and workspaces.

---

## Extension UI & Features

### 1. Status Bar Display
- **Format**: `AGY: xxx.xk handoff xxxk` (e.g. `AGY: 142.0k handoff 150k` or `AGY: 142.0k handoff off`).
- **Dynamic Color Warnings**:
  - **Normal**: Safe working zone (> 25k tokens away from compression).
  - **Yellow**: Within 25k tokens of compression or 15k tokens of handoff target.
  - **Red**: Within 10k tokens of compression or exceeding the handoff target.
- **Hover Tooltip**: Displays total tokens, user/model/thinking token breakdown, and exact headroom until next compression.

### 2. Click Menu Actions
Click the status bar widget to open the quick configuration menu:
- **Inject handoff message next turn**: Instructs the assistant to wrap up and write a handoff summary on its immediate next turn, without changing your long-term threshold.
- **View detailed token report**: Opens an output channel displaying full session analytics.
- **Preset Targets**: Quick selection for `150k` (pre-C1), `140k`, `160k`, `320k` (pre-C2), or `off`.
- **Custom Input**: Type any custom threshold (e.g. `145k`, `200000`, `off`).

---

## Daemon Architecture

```
                  ┌──────────────────────────────────────────────┐
                  │          Antigravity Session                 │
                  │   ~/.gemini/antigravity/brain/<id>/...       │
                  │       transcript_full.jsonl (Raw Logs)       │
                  └───────────────────────┬──────────────────────┘
                                          │
                            fsnotify Event Queue (Zero-CPU idle)
                                          ▼
                  ┌──────────────────────────────────────────────┐
                  │             Token Daemon (Go)                │
                  │        agy-token-monitor daemon              │
                  └───────────────┬──────────────┬───────────────┘
                                  │              │
                   every turn/step│              │ debounced (3s)
                                  ▼              ▼
           ┌────────────────────────────┐  ┌────────────────────────────┐
           │ Pure-Go SQLite DB          │  │ Annotations / Summaries    │
           │ ~/.gemini/.../metrics.db   │  │ conversation_summaries.db  │
           └──────────────┬─────────────┘  └────────────────────────────┘
                          │
            every 1s poll │
                          ▼
           ┌────────────────────────────┐
           │ VS Code Status Bar Widget  │
           │  AGY: 154.2k handoff 150k  │
           └────────────────────────────┘
```

---

## Unified Go Binary & CLI Usage (`agy-token-monitor` / `agy-tokens`)

The unified binary provides subcommands for running the background daemon, inspecting token stats, and exporting data:

```bash
# Build the unified binary:
go build -o agy-token-monitor .

# Start the background daemon service:
agy-token-monitor daemon
agy-token-monitor daemon --once    # Single-pass reconciliation and sync

# View active conversation token summary:
agy-token-monitor stats
# (or simply: agy-token-monitor / agy-tokens)

# View token metrics grouped by workspace/project:
agy-token-monitor stats -p

# View top conversations ranked by token consumption:
agy-token-monitor stats -t 10
agy-token-monitor stats -t 5 --filter-project my-app

# Inspect a specific conversation:
agy-token-monitor stats -c <conversation-id>

# Run raw SQL queries:
agy-token-monitor stats -q "SELECT title, est_total_tokens, est_thinking_tokens FROM conversation_summary ORDER BY est_total_tokens DESC LIMIT 5;"

# Export conversation metrics to JSON or CSV:
agy-token-monitor export --json --out tokens.json
agy-token-monitor export --csv --out tokens.csv
agy-token-monitor export --json -c <conversation-id>
agy-token-monitor export --csv -c <conversation-id>
```

---

## Configuration Settings

The extension contributes first-class configuration options under `antigravity.tokenMonitor`:

| Setting | Default | Description |
|---|---|---|
| `antigravity.tokenMonitor.handoffTarget` | `150000` | Token limit for handoff recommendation (`0` to disable). |
| `antigravity.tokenMonitor.pollIntervalMs` | `1000` | Status bar update frequency in milliseconds (500–10000). |
| `antigravity.tokenMonitor.enableWarningColors` | `true` | Show color-coded status bar (Yellow/Red) near limits. |
| `antigravity.tokenMonitor.compressionHeadroomBuffer` | `25000` | Token warning buffer prior to internal compression. |

Settings can be changed via the VS Code Settings Editor (`Ctrl+,`), `settings.json`, or the QuickPick click menu.


---

## Service Management

The monitor runs as a systemd user daemon:

```bash
# Check status
systemctl --user status agy-token-monitor.service

# Restart daemon
systemctl --user restart agy-token-monitor.service

# View live stream logs
journalctl --user -u agy-token-monitor.service -f
```

---

## Roadmap & Known Issues

See [ISSUES.md](./ISSUES.md) for details on planned enhancements:
- Kernel I/O modernization (`epoll` / `io_uring` implementation).
- Native VS Code `settings.json` schema integration.
- Built-in CSV / JSON / Parquet export tools in the CLI.
- Dynamic missed-handoff target auto-advancement.
