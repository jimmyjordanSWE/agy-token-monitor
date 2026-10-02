# Gemini 3.8 Token Monitor & Automatic Handoff

A lightweight context tracker, compression monitor, and automatic handoff extension for Google Antigravity in VS Code.

## Overview

Gemini 3.8 compresses context at roughly **~170,000 tokens** (and subsequent cycles at ~340k, ~510k, ~680k). When internal compression triggers:
* Past turns are compacted into high-level summaries.
* Prefix prompt caches are invalidated.
* Subtleties, constraints, and detailed code context from earlier turns get lost.

This extension enables you to perform an orderly handoff before compression occurs, letting you transition into a clean context with a complete summary intact.

*(Note: Future versions will support additional models).*

---

## How It Works

Tokens are estimated within **~2%** using a calibrated **3.8 characters per token** formula tailored for code, JSON tool schemas, and indentation. 

* **Sub-millisecond execution**: Runs in-memory in pure JavaScript with zero lag.
* **Zero dependencies**: No external daemons, background services, or compilers required.

---

## How to Use It

### 1. Live Status Bar Widget
Look at the bottom tray in VS Code:
* Displays active tokens and target: `AGY: 89k handoff 150k`
* **Color warnings**: Shifts from Normal &rarr; Yellow (within 25k of compression) &rarr; Red (within 10k or past target).
* **Hover tooltip**: Shows breakdowns for User, Model, and Thinking tokens, plus exact headroom remaining.

### 2. Status Bar Click Menu
Click the status bar item to open quick actions:
* **Inject handoff message next turn**: Commands the agent to summarize and hand off on its immediate next response.
* **Edit handoff message template**: Customize the macro-level handoff prompt injected to the assistant.
* **View detailed token report**: Opens an output panel with full session analytics.
* **Change target threshold**: Select presets (140k, 150k, 160k, 320k) or enter any custom token number.
* **Tag all past conversations**: Scans all past sessions in ~250ms and prefixes token counts to titles in the history sidebar (e.g. `[89k] Fix Parser Bug`).
* **Remove token tags**: One-click cleanup to strip `[xxk]` prefixes and restore clean original titles.

---

## Configuration Settings

Available under `antigravity.tokenMonitor` in Settings (`Ctrl+,`):

| Setting | Default | Description |
|---|---|---|
| `antigravity.tokenMonitor.handoffTarget` | `150000` | Token threshold to trigger handoff recommendation (`0` to disable). |
| `antigravity.tokenMonitor.enableWarningColors` | `true` | Show color-coded warnings (Yellow/Red) near limits. |
| `antigravity.tokenMonitor.compressionHeadroomBuffer` | `25000` | Buffer tokens before compression milestone to trigger early yellow warning. |
| `antigravity.tokenMonitor.handoffMessage` | *(Macro prompt)* | Custom handoff directive injected to the agent. Supports `{CURRENT_TOKENS}`, `{CLIFF_TOKENS}`, `{BUFFER_LEFT}`. |

---

## License

MIT License &copy; 2026 Jimmy Jordan
