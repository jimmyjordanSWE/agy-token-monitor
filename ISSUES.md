# Issues & Future Enhancements

This document tracks technical debt, architecture refinements, and roadmap items for the `agy-token-monitor` extension and daemon.

---

## 1. Generalize Documentation & Remove Session-Specific References [DONE]
- **Status**: Completed. All references in `README.md` and configuration docs generalized for public/team consumption.

---

## 2. Linux Kernel I/O Modernization (`epoll` / `io_uring` instead of `inotify`)
- **Problem**: High-frequency directory polling / standard `inotify` incurs user-kernel context switch overhead and descriptor scaling limits across nested conversation brain trees.
- **Improvement**:
  - Replace naive polling/inotify with native Linux `epoll` on directory descriptor events, or leverage modern `io_uring` (`IORING_OP_READV` / registered ring buffers) for asynchronous zero-copy ingestion of `transcript_full.jsonl` tails.
  - Implement a lightweight C/Rust or Python ctypes/cffi kernel wrapper to handle multi-file stream monitoring at sub-millisecond latencies with minimal CPU footprint.

---

## 3. Native VS Code Settings Integration [DONE]
- **Status**: Completed. Contributed `antigravity.tokenMonitor` settings schema in `package.json`:
  - `handoffTarget`: default `150000`
  - `pollIntervalMs`: default `1000`
  - `enableWarningColors`: default `true`
  - `compressionHeadroomBuffer`: default `25000`
- Both VS Code settings UI and SQLite config are kept synchronized bi-directionally, with instant reactive updates via `vscode.workspace.onDidChangeConfiguration`.

---

## 4. CLI Data Export & Analytics (`agy-tokens --export`) [DONE]
- **Status**: Completed. Added `--export json` and `--export csv` flags to `cli.py` with optional `--out <path>` destination:
  - `agy-tokens --export json`
  - `agy-tokens --export csv --out metrics.csv`
  - Single conversation detailed step export: `agy-tokens -c <id> --export json`

---

## 5. Smarter Missed-Handoff Recovery Logic [DONE]
- **Status**: Completed. QuickPick menu now dynamically detects when active tokens exceed the current handoff threshold, offering a prominent one-click action:
  - `$(debug-step-over) Advance handoff to Next Cycle (<target>k)`
  - Direct presets added for Pre-Cycle 1 (150k), Pre-Cycle 2 (320k), Pre-Cycle 3 (490k), and Pre-Cycle 4 (660k).

---

## 6. Active Conversation Detection When Clicking '+' (New Chat)
- **Problem**: When the user clicks the '+' button in the Antigravity sidebar to start a new chat, the status bar in the bottom tray still reflects the token count of the previous chat.
- **Goal**: The status bar should immediately reflect whichever specific conversation is currently active and being viewed in the chat pane (resetting to 0k on '+' new chat).
- **Investigation / Action**:
  - Determine how the extension can detect the currently focused/active conversation ID (e.g. by listening to extension webview messages, inspecting workspace state `lastConversationId`, or polling `annotations/*.pbtxt` `last_user_view_time`).
  - Wire this active conversation ID into the status bar query so switching tabs/conversations immediately updates the bottom bar.

---

## 7. Token Counts in History Sidebar (Event-Driven Dirty Queue) [DONE]
- **Status**: Completed. Replaced naive periodic polling with an event-driven dirty-marking queue:
  - Added `last_synced_tokens` tracking to `token_metrics.db`.
  - Conversations are only enqueued when file size changes (`st_size > last_offset`).
  - Flush queue debounces updates (3s) and tags `conversation_summaries.db` / `annotations/` only when token metrics actually change.
  - Startup reconciles any unsynced offline conversations once; idle state performs zero database updates or file reads.
