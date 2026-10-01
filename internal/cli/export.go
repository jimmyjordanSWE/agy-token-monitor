package cli

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"agy-token-monitor/internal/config"
	"agy-token-monitor/internal/db"
)

// ExportOptions holds options for JSON / CSV export.
type ExportOptions struct {
	Format         string // "json" or "csv"
	OutPath        string
	ConversationID string
}

// SingleConvExport represents export payload when a specific conversation is selected.
type SingleConvExport struct {
	Summary db.ConversationSummary `json:"summary"`
	Steps   []db.MessageStep       `json:"steps"`
}

// RunExport exports conversation summaries or detailed message steps.
func RunExport(cfg *config.Config, database *sql.DB, opts ExportOptions) error {
	format := opts.Format
	if format == "" {
		format = "json"
	}

	var outputBytes []byte
	var err error

	if opts.ConversationID != "" {
		outputBytes, err = exportSingleConversation(database, opts.ConversationID, format)
	} else {
		outputBytes, err = exportAllSummaries(database, format)
	}

	if err != nil {
		return err
	}

	if opts.OutPath != "" {
		if err := os.WriteFile(opts.OutPath, outputBytes, 0644); err != nil {
			return fmt.Errorf("failed to write export file: %w", err)
		}
		fmt.Printf("Exported metrics to: %s\n", opts.OutPath)
	} else {
		fmt.Print(string(outputBytes))
	}

	return nil
}

func exportSingleConversation(database *sql.DB, convID, format string) ([]byte, error) {
	var summary db.ConversationSummary
	var lastSynced sql.NullInt64

	err := database.QueryRow(`
		SELECT conversation_id, COALESCE(title, ''), COALESCE(project_name, ''),
		       total_steps, total_chars, est_total_tokens, est_user_tokens,
		       est_model_tokens, est_tool_tokens, est_thinking_tokens,
		       last_updated, last_synced_tokens
		FROM conversation_summary
		WHERE conversation_id = ?
	`, convID).Scan(
		&summary.ConversationID, &summary.Title, &summary.ProjectName,
		&summary.TotalSteps, &summary.TotalChars, &summary.EstTotalTokens,
		&summary.EstUserTokens, &summary.EstModelTokens, &summary.EstToolTokens,
		&summary.EstThinkingTokens, &summary.LastUpdated, &lastSynced,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("conversation not found: %s", convID)
		}
		return nil, err
	}
	if lastSynced.Valid {
		summary.LastSyncedTokens = lastSynced.Int64
	}

	rows, err := database.Query(`
		SELECT id, conversation_id, step_index, source, type, status, created_at,
		       content_chars, thinking_chars, tool_chars, est_tokens
		FROM message_steps
		WHERE conversation_id = ?
		ORDER BY step_index ASC
	`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []db.MessageStep
	for rows.Next() {
		var s db.MessageStep
		if err := rows.Scan(
			&s.ID, &s.ConversationID, &s.StepIndex, &s.Source, &s.Type, &s.Status,
			&s.CreatedAt, &s.ContentChars, &s.ThinkingChars, &s.ToolChars, &s.EstTokens,
		); err == nil {
			steps = append(steps, s)
		}
	}

	if format == "json" {
		payload := SingleConvExport{
			Summary: summary,
			Steps:   steps,
		}
		return json.MarshalIndent(payload, "", "  ")
	}

	// CSV format for steps
	var buf []byte
	w := csv.NewWriter(&bytesWriter{b: &buf})
	_ = w.Write([]string{
		"id", "conversation_id", "step_index", "source", "type", "status",
		"created_at", "content_chars", "thinking_chars", "tool_chars", "est_tokens",
	})

	for _, s := range steps {
		_ = w.Write([]string{
			strconv.FormatInt(s.ID, 10),
			s.ConversationID,
			strconv.Itoa(s.StepIndex),
			s.Source,
			s.Type,
			s.Status,
			s.CreatedAt,
			strconv.Itoa(s.ContentChars),
			strconv.Itoa(s.ThinkingChars),
			strconv.Itoa(s.ToolChars),
			strconv.FormatInt(s.EstTokens, 10),
		})
	}
	w.Flush()
	return buf, nil
}

func exportAllSummaries(database *sql.DB, format string) ([]byte, error) {
	rows, err := database.Query(`
		SELECT conversation_id, COALESCE(title, ''), COALESCE(project_name, ''),
		       total_steps, total_chars, est_total_tokens, est_user_tokens,
		       est_model_tokens, est_tool_tokens, est_thinking_tokens,
		       last_updated, last_synced_tokens
		FROM conversation_summary
		ORDER BY est_total_tokens DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []db.ConversationSummary
	for rows.Next() {
		var s db.ConversationSummary
		var lastSynced sql.NullInt64
		if err := rows.Scan(
			&s.ConversationID, &s.Title, &s.ProjectName,
			&s.TotalSteps, &s.TotalChars, &s.EstTotalTokens,
			&s.EstUserTokens, &s.EstModelTokens, &s.EstToolTokens,
			&s.EstThinkingTokens, &s.LastUpdated, &lastSynced,
		); err == nil {
			if lastSynced.Valid {
				s.LastSyncedTokens = lastSynced.Int64
			}
			summaries = append(summaries, s)
		}
	}

	if format == "json" {
		return json.MarshalIndent(summaries, "", "  ")
	}

	// CSV format for summaries
	var buf []byte
	w := csv.NewWriter(&bytesWriter{b: &buf})
	_ = w.Write([]string{
		"conversation_id", "title", "project_name", "total_steps", "total_chars",
		"est_total_tokens", "est_user_tokens", "est_model_tokens", "est_tool_tokens",
		"est_thinking_tokens", "last_updated", "last_synced_tokens",
	})

	for _, s := range summaries {
		_ = w.Write([]string{
			s.ConversationID,
			s.Title,
			s.ProjectName,
			strconv.Itoa(s.TotalSteps),
			strconv.FormatInt(s.TotalChars, 10),
			strconv.FormatInt(s.EstTotalTokens, 10),
			strconv.FormatInt(s.EstUserTokens, 10),
			strconv.FormatInt(s.EstModelTokens, 10),
			strconv.FormatInt(s.EstToolTokens, 10),
			strconv.FormatInt(s.EstThinkingTokens, 10),
			s.LastUpdated.Format("2006-01-02 15:04:05"),
			strconv.FormatInt(s.LastSyncedTokens, 10),
		})
	}
	w.Flush()
	return buf, nil
}

type bytesWriter struct {
	b *[]byte
}

func (w *bytesWriter) Write(p []byte) (n int, err error) {
	*w.b = append(*w.b, p...)
	return len(p), nil
}
