package daemon

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"agy-token-monitor/internal/config"
	"agy-token-monitor/internal/db"
	"agy-token-monitor/internal/parser"
)

// Daemon coordinates background log tailing, dirty-queue title updating, and handoff injection.
type Daemon struct {
	cfg                *config.Config
	database           *sql.DB
	fileOffsets        map[string]int64
	lastTitleUpdate    map[string]time.Time
	dirtyConversations map[string]bool
	lastFlushedTime    map[string]time.Time
	knownTranscripts   map[string]string
	lastBrainMtime     time.Time
	watchedDirs        map[string]bool
	watcher            *fsnotify.Watcher
	mu                 sync.Mutex
}

// New creates an initialized Daemon instance.
func New(cfg *config.Config, database *sql.DB) (*Daemon, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	return &Daemon{
		cfg:                cfg,
		database:           database,
		fileOffsets:        make(map[string]int64),
		lastTitleUpdate:    make(map[string]time.Time),
		dirtyConversations: make(map[string]bool),
		lastFlushedTime:    make(map[string]time.Time),
		knownTranscripts:   make(map[string]string),
		watchedDirs:        make(map[string]bool),
		watcher:            watcher,
	}, nil
}

// Close cleans up database and fsnotify resources.
func (d *Daemon) Close() {
	if d.watcher != nil {
		_ = d.watcher.Close()
	}
}

// MarkDirty enqueues a conversation ID for debounced sidebar title update.
func (d *Daemon) MarkDirty(convID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dirtyConversations[convID] = true
}

// FlushDirtyQueue writes pending title updates to SQLite and pbtxt files.
func (d *Daemon) FlushDirtyQueue(debounce time.Duration, force bool) {
	d.mu.Lock()
	if len(d.dirtyConversations) == 0 {
		d.mu.Unlock()
		return
	}

	now := time.Now()
	var candidates []string
	for convID := range d.dirtyConversations {
		if !force && now.Sub(d.lastFlushedTime[convID]) < debounce {
			continue
		}
		candidates = append(candidates, convID)
	}
	d.mu.Unlock()

	for _, convID := range candidates {
		var estTokens, lastSynced sql.NullInt64
		err := d.database.QueryRow(
			"SELECT est_total_tokens, last_synced_tokens FROM conversation_summary WHERE conversation_id = ?",
			convID,
		).Scan(&estTokens, &lastSynced)
		if err != nil {
			d.mu.Lock()
			delete(d.dirtyConversations, convID)
			d.mu.Unlock()
			continue
		}

		tokens := estTokens.Int64
		synced := int64(-1)
		if lastSynced.Valid {
			synced = lastSynced.Int64
		}

		if tokens != synced {
			parser.UpdateConversationTitleWithTokens(convID, d.cfg.BaseDir, tokens)
			_, _ = d.database.Exec(
				"UPDATE conversation_summary SET last_synced_tokens = ? WHERE conversation_id = ?",
				tokens, convID,
			)
		}

		d.mu.Lock()
		d.lastFlushedTime[convID] = now
		delete(d.dirtyConversations, convID)
		d.mu.Unlock()
	}
}

// InitDirtyState discovers unsynced conversations and synchronizes them on startup.
func (d *Daemon) InitDirtyState() {
	rows, err := d.database.Query(`
		SELECT conversation_id, est_total_tokens 
		FROM conversation_summary 
		WHERE last_synced_tokens IS NULL OR last_synced_tokens != est_total_tokens
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var convID string
		var tokens int64
		if err := rows.Scan(&convID, &tokens); err == nil {
			d.MarkDirty(convID)
		}
	}
	d.FlushDirtyQueue(0, true)
}

// CheckHandoffInjection inspects active conversation tokens and injects/removes handoff rules.
func (d *Daemon) CheckHandoffInjection() {
	var convID string
	var totalTokens int64
	err := d.database.QueryRow(`
		SELECT conversation_id, est_total_tokens 
		FROM conversation_summary 
		ORDER BY last_updated DESC LIMIT 1
	`).Scan(&convID, &totalTokens)
	if err != nil {
		return
	}

	target := db.GetHandoffTarget(d.database)
	manualFlag := db.GetManualHandoffFlag(d.database)

	if manualFlag || (target > 0 && int(totalTokens) >= target) {
		_ = os.MkdirAll(d.cfg.RulesDir, 0755)
		var reason string
		if manualFlag {
			reason = "User requested immediate handoff."
		} else {
			reason = fmt.Sprintf("Tokens (%s) exceeded target (%s).", formatComma(totalTokens), formatComma(int64(target)))
		}

		content := fmt.Sprintf(`---
description: Critical automatic context handoff directive
---

# ⚠️ CRITICAL CONTEXT LIMIT DIRECTIVE (%s)
Notice: The current conversation has reached its context handoff limit.

## INSTRUCTIONS FOR ASSISTANT:
1. Complete your immediate sub-task cleanly. Do NOT initiate new large refactors or long investigative tangents.
2. Ensure working state is verified (git status, clean files).
3. Conclude your turn with a clear, structured **HANDOFF SUMMARY**:
   - **Status & Accomplishments**: What was just completed.
   - **Key Context / Files**: Specific files modified.
   - **Next Action**: The immediate next step to take.
   - **Continuation Prompt**: A copy-pasteable prompt for the user to provide when opening the new chat.
`, reason)

		_ = os.WriteFile(d.cfg.HandoffRulePath, []byte(content), 0644)
	} else {
		// Low token count and no manual flag: clear the handoff rule
		if _, err := os.Stat(d.cfg.HandoffRulePath); err == nil {
			_ = os.Remove(d.cfg.HandoffRulePath)
		}
	}
}

func formatComma(n int64) string {
	in := fmt.Sprintf("%d", n)
	var out []byte
	l := len(in)
	for i, c := range in {
		if i > 0 && (l-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	return string(out)
}

// GetTranscriptFiles scans the brain directory only when its mtime changes.
func (d *Daemon) GetTranscriptFiles() map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()

	stat, err := os.Stat(d.cfg.BrainDir)
	if err != nil {
		return d.knownTranscripts
	}

	currentMtime := stat.ModTime()
	if !currentMtime.Equal(d.lastBrainMtime) || len(d.knownTranscripts) == 0 {
		newMap := make(map[string]string)
		pattern := filepath.Join(d.cfg.BrainDir, "*", ".system_generated", "logs", "transcript_full.jsonl")
		matches, err := filepath.Glob(pattern)
		if err == nil {
			for _, match := range matches {
				parts := strings.Split(match, string(filepath.Separator))
				brainIdx := -1
				for i, p := range parts {
					if p == "brain" {
						brainIdx = i
						break
					}
				}
				if brainIdx >= 0 && brainIdx+1 < len(parts) {
					cID := parts[brainIdx+1]
					newMap[cID] = match
				}
			}
		}
		d.knownTranscripts = newMap
		d.lastBrainMtime = currentMtime
	}

	result := make(map[string]string, len(d.knownTranscripts))
	for k, v := range d.knownTranscripts {
		result[k] = v
	}
	return result
}

// ProcessTranscriptFile streams newly appended JSON lines from transcript_full.jsonl.
func (d *Daemon) ProcessTranscriptFile(convID, filePath string) {
	stat, err := os.Stat(filePath)
	if err != nil {
		return
	}

	d.mu.Lock()
	lastOffset := d.fileOffsets[filePath]
	currentSize := stat.Size()

	if currentSize < lastOffset {
		lastOffset = 0
	}

	if currentSize == lastOffset {
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()

	file, err := os.Open(filePath)
	if err != nil {
		return
	}
	defer file.Close()

	if _, err := file.Seek(lastOffset, io.SeekStart); err != nil {
		return
	}

	reader := bufio.NewReader(file)
	var newOffset int64 = lastOffset

	tx, err := d.database.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
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
	`)
	if err != nil {
		return
	}
	defer stmt.Close()

	stepCount := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			newOffset += int64(len(line))
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) > 0 {
				step, err := parser.ParseStep(trimmed)
				if err == nil {
					_, _ = stmt.Exec(
						convID,
						step.StepIndex,
						step.Source,
						step.Type,
						step.Status,
						step.CreatedAt,
						step.ContentChars,
						step.ThinkingChars,
						step.ToolChars,
						step.EstTokens,
					)
					stepCount++
				}
			}
		}
		if readErr != nil {
			break
		}
	}

	if stepCount > 0 {
		if err := tx.Commit(); err != nil {
			return
		}
	}

	d.mu.Lock()
	d.fileOffsets[filePath] = newOffset
	d.mu.Unlock()

	// Update conversation summary aggregate
	var (
		totalSteps     int
		totalChars     int64
		totalTokens    int64
		userTokens     int64
		modelTokens    int64
		toolTokens     int64
		thinkingTokens int64
	)

	err = d.database.QueryRow(`
		SELECT 
			COUNT(*),
			COALESCE(SUM(content_chars + thinking_chars + tool_chars), 0),
			COALESCE(SUM(est_tokens), 0),
			COALESCE(SUM(CASE WHEN source = 'USER_EXPLICIT' THEN est_tokens ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN source = 'MODEL' THEN est_tokens ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN tool_chars > 0 THEN est_tokens ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN thinking_chars > 0 THEN est_tokens ELSE 0 END), 0)
		FROM message_steps
		WHERE conversation_id = ?
	`, convID).Scan(
		&totalSteps, &totalChars, &totalTokens,
		&userTokens, &modelTokens, &toolTokens, &thinkingTokens,
	)
	if err != nil {
		return
	}

	title := parser.GetConversationTitle(convID, d.cfg.BaseDir)
	projectName := parser.DetectProject(convID, d.cfg.BrainDir)

	_, _ = d.database.Exec(`
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
	`, convID, title, projectName, totalSteps, totalChars,
		totalTokens, userTokens, modelTokens, toolTokens, thinkingTokens)

	d.MarkDirty(convID)
}

// AddWatchDir registers a directory with fsnotify watcher if not already watched.
func (d *Daemon) AddWatchDir(dir string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.watchedDirs[dir] {
		return
	}
	if err := d.watcher.Add(dir); err == nil {
		d.watchedDirs[dir] = true
	}
}

// RefreshWatches scans existing and new brain folders to register them with fsnotify.
func (d *Daemon) RefreshWatches() {
	if _, err := os.Stat(d.cfg.BrainDir); err != nil {
		return
	}
	d.AddWatchDir(d.cfg.BrainDir)

	entries, err := os.ReadDir(d.cfg.BrainDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			convDir := filepath.Join(d.cfg.BrainDir, entry.Name())
			logsDir := filepath.Join(convDir, ".system_generated", "logs")
			if _, err := os.Stat(logsDir); err == nil {
				d.AddWatchDir(logsDir)
			}
		}
	}
}

// SyncAll performs a complete one-pass sync across all discovered conversations.
func (d *Daemon) SyncAll() {
	transcripts := d.GetTranscriptFiles()
	for convID, filePath := range transcripts {
		d.ProcessTranscriptFile(convID, filePath)
	}
	d.FlushDirtyQueue(0, true)
	d.CheckHandoffInjection()
}

// Run starts the daemon event loop using fsnotify and periodic queue flush.
func (d *Daemon) Run(ctx context.Context, once bool) error {
	d.InitDirtyState()
	d.SyncAll()

	if once {
		log.Println("One-pass synchronization complete.")
		return nil
	}

	d.RefreshWatches()
	log.Println("AGY Token Monitor Daemon running (fsnotify event-driven dirty queue)...")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	watchRefreshTicker := time.NewTicker(10 * time.Second)
	defer watchRefreshTicker.Stop()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case <-ctx.Done():
			d.FlushDirtyQueue(0, true)
			return nil

		case sig := <-sigChan:
			log.Printf("Received signal %v, shutting down...", sig)
			d.FlushDirtyQueue(0, true)
			return nil

		case event, ok := <-d.watcher.Events:
			if !ok {
				return nil
			}
			// If a new conversation or directory was created inside brain, refresh watches
			if event.Has(fsnotify.Create) {
				if strings.HasPrefix(event.Name, d.cfg.BrainDir) {
					d.RefreshWatches()
				}
			}

			// If transcript_full.jsonl or transcript.jsonl was modified/created
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				if strings.HasSuffix(event.Name, "transcript_full.jsonl") {
					parts := strings.Split(event.Name, string(filepath.Separator))
					brainIdx := -1
					for i, p := range parts {
						if p == "brain" {
							brainIdx = i
							break
						}
					}
					if brainIdx >= 0 && brainIdx+1 < len(parts) {
						convID := parts[brainIdx+1]
						d.ProcessTranscriptFile(convID, event.Name)
					}
				}
			}

		case err, ok := <-d.watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("fsnotify error: %v", err)

		case <-watchRefreshTicker.C:
			d.RefreshWatches()
			// Background catch-up sync
			transcripts := d.GetTranscriptFiles()
			for convID, filePath := range transcripts {
				d.ProcessTranscriptFile(convID, filePath)
			}

		case <-ticker.C:
			d.FlushDirtyQueue(3*time.Second, false)
			d.CheckHandoffInjection()
		}
	}
}
