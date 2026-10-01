package cli

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	"agy-token-monitor/internal/config"
)

// StatsOptions specifies display and filter flags for stats inspector.
type StatsOptions struct {
	ConversationID string
	Projects       bool
	Top            int
	FilterProject  string
	Query          string
}

// FormatTokens converts a token count into a human-readable suffix (k / M).
func FormatTokens(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000.0)
	} else if n >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1_000.0)
	}
	return fmt.Sprintf("%d", n)
}

func formatNumberWithCommas(n int64) string {
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

// GetCurrentConvID discovers the active conversation ID from annotations or recent DB updates.
func GetCurrentConvID(cfg *config.Config, database *sql.DB) string {
	bestID := ""
	var bestTime int64

	// 1. Check annotations pbtxt mtime
	if entries, err := os.ReadDir(cfg.AnnotationsDir); err == nil {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".pbtxt") {
				info, err := entry.Info()
				if err == nil && info.ModTime().UnixNano() > bestTime {
					bestTime = info.ModTime().UnixNano()
					bestID = strings.TrimSuffix(entry.Name(), ".pbtxt")
				}
			}
		}
	}

	if bestID != "" {
		return bestID
	}

	// 2. Fallback to conversation_summary most recent
	if database != nil {
		var convID string
		err := database.QueryRow("SELECT conversation_id FROM conversation_summary ORDER BY last_updated DESC LIMIT 1").Scan(&convID)
		if err == nil && convID != "" {
			return convID
		}
	}

	return ""
}

// RunStats routes to projects breakdown, top conversations, raw query, or single conversation view.
func RunStats(cfg *config.Config, database *sql.DB, opts StatsOptions) error {
	if opts.Query != "" {
		return RunQuery(database, opts.Query)
	}
	if opts.Projects {
		return ShowProjects(database)
	}
	if opts.Top > 0 || opts.FilterProject != "" {
		limit := opts.Top
		if limit <= 0 {
			limit = 10
		}
		return ShowTop(database, limit, opts.FilterProject)
	}

	convID := opts.ConversationID
	if convID == "" {
		convID = GetCurrentConvID(cfg, database)
	}
	if convID == "" {
		return fmt.Errorf("no active conversation found. Run the daemon first or specify -c <conv-id>")
	}

	return ShowSummary(database, convID)
}

// ShowSummary prints the detailed token breakdown for a conversation.
func ShowSummary(database *sql.DB, convID string) error {
	var (
		title             string
		projectName       sql.NullString
		totalSteps        int
		totalChars        int64
		estTotalTokens    int64
		estUserTokens     int64
		estModelTokens    int64
		estToolTokens     int64
		estThinkingTokens int64
	)

	err := database.QueryRow(`
		SELECT title, project_name, total_steps, total_chars, est_total_tokens,
		       est_user_tokens, est_model_tokens, est_tool_tokens, est_thinking_tokens
		FROM conversation_summary
		WHERE conversation_id = ?
	`, convID).Scan(
		&title, &projectName, &totalSteps, &totalChars, &estTotalTokens,
		&estUserTokens, &estModelTokens, &estToolTokens, &estThinkingTokens,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			fmt.Printf("No metrics recorded for conversation: %s\n", convID)
			return nil
		}
		return fmt.Errorf("query error: %w", err)
	}

	proj := "Unknown"
	if projectName.Valid && projectName.String != "" {
		proj = projectName.String
	}

	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("📊 AGY TOKEN USAGE REPORT")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("Conversation:  %s\n", title)
	fmt.Printf("Project:       %s\n", proj)
	fmt.Printf("ID:            %s\n", convID)
	fmt.Printf("Total Steps:   %d\n", totalSteps)
	fmt.Printf("Total Chars:   %s\n", formatNumberWithCommas(totalChars))
	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("Estimated Total Tokens:    %s (%s)\n", FormatTokens(estTotalTokens), formatNumberWithCommas(estTotalTokens))
	fmt.Printf("  ├─ User Prompt Tokens:   %s (%s)\n", FormatTokens(estUserTokens), formatNumberWithCommas(estUserTokens))
	fmt.Printf("  ├─ Model Output Tokens:  %s (%s)\n", FormatTokens(estModelTokens), formatNumberWithCommas(estModelTokens))
	fmt.Printf("  ├─ Model Thinking:       %s (%s)\n", FormatTokens(estThinkingTokens), formatNumberWithCommas(estThinkingTokens))
	fmt.Printf("  └─ Tool Payload Tokens:  %s (%s)\n", FormatTokens(estToolTokens), formatNumberWithCommas(estToolTokens))
	fmt.Println(strings.Repeat("=", 60))

	rows, err := database.Query(`
		SELECT step_index, source, type, est_tokens 
		FROM message_steps 
		WHERE conversation_id = ? 
		ORDER BY step_index DESC LIMIT 5
	`, convID)
	if err == nil {
		defer rows.Close()
		type stepItem struct {
			stepIndex int
			source    string
			stepType  string
			estTokens int64
		}
		var list []stepItem
		for rows.Next() {
			var s stepItem
			if err := rows.Scan(&s.stepIndex, &s.source, &s.stepType, &s.estTokens); err == nil {
				list = append(list, s)
			}
		}
		if len(list) > 0 {
			fmt.Println("\nRecent Steps:")
			// Print in ascending order
			for i := len(list) - 1; i >= 0; i-- {
				s := list[i]
				fmt.Printf("  Step #%-4d | %-13s | %-16s | %s tokens\n",
					s.stepIndex, s.source, s.stepType, FormatTokens(s.estTokens))
			}
		}
	}
	fmt.Println()
	return nil
}

// ShowProjects displays aggregated token consumption by project/workspace.
func ShowProjects(database *sql.DB) error {
	rows, err := database.Query(`
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
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	fmt.Println(strings.Repeat("=", 85))
	fmt.Println("📁 AGY TOKEN USAGE BY PROJECT / WORKSPACE")
	fmt.Println(strings.Repeat("=", 85))
	fmt.Printf("%-30s | %-6s | %-7s | %-9s | %-8s | %-8s\n",
		"Project / Workspace", "Convs", "Steps", "Total", "User", "Model")
	fmt.Println(strings.Repeat("-", 85))

	for rows.Next() {
		var (
			project     string
			convCount   int
			totalSteps  int
			totalTokens int64
			userTokens  int64
			modelTokens int64
		)
		if err := rows.Scan(&project, &convCount, &totalSteps, &totalTokens, &userTokens, &modelTokens); err == nil {
			fmt.Printf("%-30s | %-6d | %-7d | %-9s | %-8s | %-8s\n",
				truncateStr(project, 30), convCount, totalSteps,
				FormatTokens(totalTokens), FormatTokens(userTokens), FormatTokens(modelTokens))
		}
	}
	fmt.Println(strings.Repeat("=", 85))
	return nil
}

// ShowTop prints the top N conversations ranked by total token usage.
func ShowTop(database *sql.DB, limit int, filterProject string) error {
	query := `
		SELECT conversation_id, title, COALESCE(project_name, 'Unknown') as project_name,
		       total_steps, est_total_tokens, est_user_tokens, est_model_tokens 
		FROM conversation_summary
	`
	var args []any
	if filterProject != "" {
		query += " WHERE project_name LIKE ? "
		args = append(args, "%"+filterProject+"%")
	}
	query += " ORDER BY est_total_tokens DESC LIMIT ?"
	args = append(args, limit)

	rows, err := database.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	header := fmt.Sprintf("🏆 TOP %d CONVERSATIONS BY TOKEN CONSUMPTION", limit)
	if filterProject != "" {
		header += fmt.Sprintf(" (Project: %s)", filterProject)
	}

	fmt.Println(strings.Repeat("=", 95))
	fmt.Println(header)
	fmt.Println(strings.Repeat("=", 95))
	fmt.Printf("%-35s | %-20s | %-6s | %-8s | %-7s | %-7s\n",
		"Title", "Project", "Steps", "Total", "User", "Model")
	fmt.Println(strings.Repeat("-", 95))

	for rows.Next() {
		var (
			convID      string
			title       sql.NullString
			proj        string
			totalSteps  int
			totalTokens int64
			userTokens  int64
			modelTokens int64
		)
		if err := rows.Scan(&convID, &title, &proj, &totalSteps, &totalTokens, &userTokens, &modelTokens); err == nil {
			t := "Untitled"
			if title.Valid && title.String != "" {
				t = title.String
			}
			fmt.Printf("%-35s | %-20s | %-6d | %-8s | %-7s | %-7s\n",
				truncateStr(t, 35), truncateStr(proj, 20), totalSteps,
				FormatTokens(totalTokens), FormatTokens(userTokens), FormatTokens(modelTokens))
		}
	}
	fmt.Println(strings.Repeat("=", 95))
	return nil
}

// RunQuery runs a raw SQL query against the database and prints rows.
func RunQuery(database *sql.DB, query string) error {
	rows, err := database.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	vals := make([]any, len(cols))
	valPtrs := make([]any, len(cols))
	for i := range vals {
		valPtrs[i] = &vals[i]
	}

	for rows.Next() {
		if err := rows.Scan(valPtrs...); err != nil {
			return err
		}
		var parts []string
		for _, v := range vals {
			if v == nil {
				parts = append(parts, "NULL")
			} else if b, ok := v.([]byte); ok {
				parts = append(parts, string(b))
			} else {
				parts = append(parts, fmt.Sprintf("%v", v))
			}
		}
		fmt.Println(strings.Join(parts, "\t"))
	}
	return nil
}

func truncateStr(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
