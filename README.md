# AGY Token Monitor & Automatic Handoff

A lightweight, non-intrusive context tracker, compression monitor, and automatic handoff extension for Google Antigravity in VS Code.

---

## Why This Extension Exists

When working with large language models in autonomous agentic loops (such as **Gemini 3.8** in Google Antigravity), context windows expand rapidly as tool calls, file diffs, and terminal outputs pile up. 

Around **~170,000 tokens** (and subsequent cycles at ~340k, ~510k, ~680k), the runtime triggers an **internal context compression pass**. 

When compression happens:
1. **Prompt Caching is Invalidated**: Past turns are summarized and compacted, breaking your prefix cache and resetting cache cost advantages.
2. **Information & Nuance are Lost**: Detailed code context, subtle constraints, and edge-case instructions from earlier turns get squashed into high-level summaries.
3. **Attention Dilution Sets In**: The model's reasoning precision degrades across heavily compressed histories.

**Most developers would rather start a fresh context window with a clean, structured handoff summary than continue working in a degraded, compressed session.**

This extension gives you continuous, live visibility into your token usage so you can take control before compression strikes.

---

## How It Works

### Fast, Lightweight Token Estimation (Accurate Within ~2%)
Instead of bundling a heavy 5 MB tokenizer vocabulary or running CPU-intensive BPE loops that lag your editor, this extension uses a calibrated **3.8 characters per token** ratio tailored specifically for code, JSON tool schemas, and indentation.
* **Sub-millisecond execution**: Takes < 1ms to parse hundreds of thousands of characters in memory.
* **Empirical accuracy**: Verified within **$\pm 2\%$** of the actual model context window on real Antigravity developer workloads—more than accurate enough to guard against the ~170k compression cliff.
* **Pure JavaScript**: 100% self-contained in VS Code. Zero external daemons, zero compilers, zero background services.

---

## Key Features

### 1. Live Status Bar Widget
Located right in the bottom tray of VS Code:
* **Real-time Display**: Shows active token count and configured target, e.g.:  
  `AGY: 89.2k handoff 150k`
* **Dynamic Warning Colors**:
  * **Normal**: Safe working zone (> 25k tokens from compression).
  * **Yellow**: Within 25k tokens of compression or 15k tokens of your handoff limit.
  * **Red**: Within 10k tokens of compression or exceeding your target.
* **Hover Tooltip**: Reveals total tokens, user / model / thinking token breakdowns, current compression cycle (`C0`–`C4`), and exact tokens remaining until the next cliff.

### 2. Click Menu Actions (Bottom Bar)
Click the status bar item at any time to open the QuickPick menu:
* **$(sign-out) Inject handoff message next turn**: Forces the assistant to wrap up its work and generate a comprehensive handoff summary on its immediate next turn, without changing your long-term limit.
* **$(output) View detailed token report**: Opens an output channel displaying full session analytics and token breakdowns.
* **$(tag) Tag all past conversations with token counts**: Scans your entire Antigravity session history in ~250ms and prefixes token counts to titles in the sidebar (e.g., `[89k] VS Code Extension Guide`).
* **$(clear-all) Remove token tags from all conversations**: One-click cleanup to strip `[xxk]` prefixes and restore clean original titles.
* **Preset & Custom Targets**: Quick one-click presets (`140k`, `150k` Pre-C1, `160k`, `320k` Pre-C2, `490k` Pre-C3) or type any custom limit (e.g. `145k`, `200000`, or `off`).

### 3. Session Intelligence & History Sidebar Tagging
* **Instant Session Switching**: Automatically detects which chat is active in Antigravity when you switch tabs or click `+` (New Chat) by watching session view events.
* **Sidebar History Visibility**: Keep track of token-heavy tasks directly in Antigravity's conversation list.

---

## Configuration Settings

Configurable via the VS Code Settings Editor (`Ctrl+,`), your `settings.json`, or directly from the bottom bar click menu:

| Setting | Default | Description |
|---|---|---|
| `antigravity.tokenMonitor.handoffTarget` | `150000` | Token threshold to trigger handoff recommendation (`0` to disable). |
| `antigravity.tokenMonitor.enableWarningColors` | `true` | Show color-coded warnings (Yellow/Red) near limits. |
| `antigravity.tokenMonitor.compressionHeadroomBuffer` | `25000` | Buffer tokens before compression milestone to trigger early yellow warning. |

---

## Commands

Accessible from the Command Palette (`Ctrl+Shift+P` / `Cmd+Shift+P`):
* `AGY: Configure Handoff & Token Monitor`
* `AGY: Show Token Metrics Report`
* `AGY: Inject Handoff Next Turn`
* `AGY: Tag All Conversation Titles with Token Counts`
* `AGY: Remove Token Tags from All Conversation Titles`

---

## License

MIT License &copy; 2026 Jimmy Jordan
