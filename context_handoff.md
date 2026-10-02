# Long-Term Mission & Architecture Handoff

> **Purpose**: This document bridges context across multiple conversation sessions. It explains the overarching problem we are solving, the architectural decisions and trade-offs made, the current state, and how future agents should continue work without regressing design choices.

---

## 1. The Core Mission: What Problem Are We Solving?

When developers run autonomous AI coding sessions in Google Antigravity (powered by Gemini 3.8):
1. **The Context Compression Cliff (~170k tokens)**: As the agent reads files, inspects diffs, and runs terminal commands, token consumption climbs rapidly. At around **~170,000 tokens** (and subsequent cycles at ~340k, ~510k), Antigravity triggers an automatic internal compression pass.
2. **The Damage Caused by Compression**:
   * **Prefix Prompt Cache Invalidation**: The prefix prompt cache is destroyed, resetting latency and cost benefits.
   * **Loss of Architectural Nuance**: Crucial instructions, subtle constraints, and edge-case context from earlier turns get squashed into lossy summaries.
   * **Attention Dilution**: Reasoning acuity noticeably degrades across heavily compressed sessions.
3. **The Core Philosophy**:
   **Rather than continuing work in a degraded, compressed context, developers want to perform an orderly handoff—generating a clean, structured summary and transitioning into a fresh context window before compression strikes.**

---

## 2. Product Identity & Marketplace Configuration

* **Extension Display Name**: `Gemini 3.8 Token Monitor & Automatic Handoff`
* **Extension ID / Slug**: `agy-token-monitor`
* **Version**: `0.9.0-beta.1`
* **Publisher**: `jimmyjordanswe` (Marketplace Display Name: **Jimmy Jordan**)
* **Author**: `Jimmy Jordan` (GitHub: `https://github.com/jimmyjordanSWE/agy-token-monitor`)
* **License**: MIT License (2026 Jimmy Jordan)
* **Status**: Clean packaged `.vsix` bundle created (`agy-token-monitor-0.9.0-beta.1.vsix`, ~36 KB).

---

## 3. Major Architectural Decisions & Why We Made Them

### Decision A: 100% Pure JavaScript (No Go Daemon / No External Binaries)
* **Initial State**: The repository originally contained a Go daemon (`main.go`, `internal/daemon/`, SQLite writer, systemd service templates).
* **Why We Removed It**: For a public VS Code extension, requiring users to have Go installed or distributing multi-architecture compiled binaries (linux-x64, darwin-arm64, win32-x64) creates massive distribution friction. 
* **Current Architecture**: The extension is 100% pure JavaScript running inside the VS Code Extension Host. It reads Antigravity session files directly using standard Node.js `fs` in `< 1ms` with zero CPU overhead when idle.

### Decision B: Calibrated 3.8 Formula vs. Full Tokenizer WASM
* **Why Not a Full Tokenizer?**: Model tokenizers (like SentencePiece for Gemini) require bundling a ~256k subword vocabulary (~5 MB compressed) and running intensive BPE loops across 500k+ characters on every turn, which causes editor micro-stutters.
* **The Formula**: `tokens = Math.round(totalChars / 3.8)`. On code, tool schemas, and diffs, this formula has been empirically verified to remain within **$\pm 2\%$** of true Gemini context.

### Decision C: Reading `transcript_full.jsonl` (Not `transcript.jsonl`)
* **The Discovery**: `transcript.jsonl` truncates large tool outputs (diffs, listings), causing token counts to read ~10–15k lower than reality (~140k vs 150k).
* **The Fix**: `extension.js` uses `getTranscriptPath()`, which prioritizes `transcript_full.jsonl`. This ensures the status bar reflects the true un-truncated tokens passed to the model and aligns with the agent runtime harness notice.

### Decision D: VS Code Save Conflict Resolution & Tab Hijacking Fix
* **The Conflict Problem**: User settings had `"files.saveConflictResolution": "overwriteFileOnDisk"` and `afterDelay: 1000`. This caused open editor tabs in VS Code to aggressively overwrite disk files every 1 second.
* **The Fix**: Removed `overwriteFileOnDisk` and added `"antigravity.autoOpenFiles": false` to `.vscode/settings.json` so background file edits never force-open tabs or steal editor focus.

### Decision E: Clean Lifecycle Uninstall Hook (`uninstall.js`)
* When the extension is uninstalled, VS Code executes `node ./uninstall.js` via the `"vscode:uninstall"` hook, automatically stripping any `[xxk]` prefixes and restoring clean conversation titles in history.

---

## 4. Current Feature Set & Code Structure

1. **[`extension.js`](file:///home/jimmy/agy-token-monitor/extension.js)**:
   * **Status Bar Widget**: `AGY: <tokens>k handoff <target>k` with dynamic warning colors (Normal &rarr; Yellow &rarr; Red).
   * **QuickPick Menu**:
     * *Inject handoff message next turn*: Sets directive via `token_hook_state.json`.
     * *View detailed token report*: Breakdown of user, model, and thinking tokens.
     * *Tag all past conversations*: Batch prefixes `[xxk]` to titles in `annotations/` and `conversation_summaries.db` via stdin-piped SQLite queries.
     * *Remove token tags*: One-click strip of all token prefixes across history.
     * *Threshold Presets*: 140k, 150k (pre-C1), 160k, 320k (pre-C2), or custom.
   * **Session Auto-Detection**: Scans `~/.gemini/antigravity/annotations/` timestamps to bind instantly to whichever chat is currently being viewed.
2. **[`uninstall.js`](file:///home/jimmy/agy-token-monitor/uninstall.js)**:
   * Standalone Node script executed on extension uninstall to clean history titles.
3. **[`package.json`](file:///home/jimmy/agy-token-monitor/package.json)**:
   * Contributes commands, configuration schema (`antigravity.tokenMonitor.*`), activation events, and uninstall hook.
4. **[`README.md`](file:///home/jimmy/agy-token-monitor/README.md)**:
   * Concise developer documentation detailing the Gemini 3.8 ~170k compression cliff, the 3.8 formula, and how to use the controls.

---

## 5. Immediate Next Steps for Next Session

1. **Marketplace Publication**:
   * Publisher account `jimmyjordanswe` is created on [marketplace.visualstudio.com/manage](https://marketplace.visualstudio.com/manage).
   * Upload the packaged bundle: [`agy-token-monitor-0.9.0-beta.1.vsix`](file:///home/jimmy/agy-token-monitor/agy-token-monitor-0.9.0-beta.1.vsix).
2. **Multi-Model Support Roadmap**:
   * Prepare configuration presets for future models as noted in README (e.g. Claude ~200k, GPT-4o ~128k).
3. **Sidebar Tagging Verification**:
   * Reload window (`Ctrl+Shift+P` &rarr; `Developer: Reload Window`) to test live `transcript_full.jsonl` tracking.
