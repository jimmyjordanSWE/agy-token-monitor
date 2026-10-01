package parser

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "modernc.org/sqlite"
)

// CharsPerToken is the average characters per token across natural language, code, and JSON.
const CharsPerToken = 3.8

var (
	tokenTagRegex = regexp.MustCompile(`(?i)^\[[\d\.]+[kM]?\s*(tokens)?\]\s*`)
	pbtxtTitleRe  = regexp.MustCompile(`title\s*:\s*"([^"]+)"`)
)

// StepParsed contains extracted metrics for a transcript step.
type StepParsed struct {
	StepIndex     int
	Source        string
	Type          string
	Status        string
	CreatedAt     string
	ContentChars  int
	ThinkingChars int
	ToolChars     int
	EstTokens     int64
}

// EstimateTokens calculates estimated token count from character count.
func EstimateTokens(chars int) int64 {
	if chars <= 0 {
		return 0
	}
	tokens := int64(math.Round(float64(chars) / CharsPerToken))
	if tokens < 1 {
		return 1
	}
	return tokens
}

type rawStep struct {
	StepIndex int             `json:"step_index"`
	Source    string          `json:"source"`
	Type      string          `json:"type"`
	Status    string          `json:"status"`
	CreatedAt string          `json:"created_at"`
	Content   *string         `json:"content"`
	Thinking  *string         `json:"thinking"`
	ToolCalls json.RawMessage `json:"tool_calls"`
}

// ParseStep parses a JSONL line into StepParsed.
func ParseStep(rawLine []byte) (*StepParsed, error) {
	if len(bytesTrim(rawLine)) == 0 {
		return nil, fmt.Errorf("empty line")
	}

	var raw rawStep
	if err := json.Unmarshal(rawLine, &raw); err != nil {
		return nil, err
	}

	source := raw.Source
	if source == "" {
		source = "UNKNOWN"
	}
	stepType := raw.Type
	if stepType == "" {
		stepType = "UNKNOWN"
	}
	status := raw.Status
	if status == "" {
		status = "DONE"
	}

	contentChars := 0
	if raw.Content != nil {
		contentChars = len(*raw.Content)
	}

	thinkingChars := 0
	if raw.Thinking != nil {
		thinkingChars = len(*raw.Thinking)
	}

	toolChars := 0
	if len(raw.ToolCalls) > 0 && string(raw.ToolCalls) != "null" {
		toolChars = len(raw.ToolCalls)
	}

	totalChars := contentChars + thinkingChars + toolChars
	tokens := EstimateTokens(totalChars)

	return &StepParsed{
		StepIndex:     raw.StepIndex,
		Source:        source,
		Type:          stepType,
		Status:        status,
		CreatedAt:     raw.CreatedAt,
		ContentChars:  contentChars,
		ThinkingChars: thinkingChars,
		ToolChars:     toolChars,
		EstTokens:     tokens,
	}, nil
}

func bytesTrim(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

// GetConversationTitle retrieves the title from conversation_summaries.db or annotations pbtxt.
func GetConversationTitle(convID string, baseAntigravityDir string) string {
	// 1. Check conversation_summaries.db
	sumDBPath := filepath.Join(baseAntigravityDir, "conversation_summaries.db")
	if _, err := os.Stat(sumDBPath); err == nil {
		db, err := sql.Open("sqlite", sumDBPath)
		if err == nil {
			var title string
			err = db.QueryRow("SELECT title FROM conversation_summaries WHERE conversation_id = ?", convID).Scan(&title)
			db.Close()
			if err == nil && title != "" {
				cleaned := strings.TrimSpace(tokenTagRegex.ReplaceAllString(title, ""))
				if cleaned != "" {
					return cleaned
				}
			}
		}
	}

	// 2. Fallback to annotations pbtxt
	pbtxtPath := filepath.Join(baseAntigravityDir, "annotations", fmt.Sprintf("%s.pbtxt", convID))
	if data, err := os.ReadFile(pbtxtPath); err == nil {
		m := pbtxtTitleRe.FindSubmatch(data)
		if len(m) > 1 {
			cleaned := strings.TrimSpace(tokenTagRegex.ReplaceAllString(string(m[1]), ""))
			if cleaned != "" {
				return cleaned
			}
		}
	}

	return "Untitled"
}

// FormatTokenTag formats token count into a tag e.g. [150k], [1.2M], [450].
func FormatTokenTag(estTotalTokens int64) string {
	if estTotalTokens >= 1_000_000 {
		return fmt.Sprintf("[%.1fM]", float64(estTotalTokens)/1_000_000.0)
	} else if estTotalTokens >= 1_000 {
		return fmt.Sprintf("[%dk]", int(math.Round(float64(estTotalTokens)/1000.0)))
	} else if estTotalTokens > 0 {
		return fmt.Sprintf("[%d]", estTotalTokens)
	}
	return "[0k]"
}

// UpdateConversationTitleWithTokens updates the sidebar title in conversation_summaries.db and annotations/*.pbtxt.
func UpdateConversationTitleWithTokens(convID, baseAntigravityDir string, estTotalTokens int64) bool {
	tokenTag := FormatTokenTag(estTotalTokens)
	updated := false

	// 1. Update in conversation_summaries.db
	sumDBPath := filepath.Join(baseAntigravityDir, "conversation_summaries.db")
	if _, err := os.Stat(sumDBPath); err == nil {
		db, err := sql.Open("sqlite", sumDBPath)
		if err == nil {
			var origTitle string
			err = db.QueryRow("SELECT title FROM conversation_summaries WHERE conversation_id = ?", convID).Scan(&origTitle)
			if err == nil && origTitle != "" {
				cleaned := strings.TrimSpace(tokenTagRegex.ReplaceAllString(origTitle, ""))
				newTitle := fmt.Sprintf("%s %s", tokenTag, cleaned)
				if newTitle != origTitle {
					_, _ = db.Exec("UPDATE conversation_summaries SET title = ? WHERE conversation_id = ?", newTitle, convID)
					updated = true
				}
			}
			db.Close()
		}
	}

	// 2. Update in annotations/*.pbtxt
	pbtxtPath := filepath.Join(baseAntigravityDir, "annotations", fmt.Sprintf("%s.pbtxt", convID))
	if data, err := os.ReadFile(pbtxtPath); err == nil {
		content := string(data)
		loc := pbtxtTitleRe.FindStringSubmatchIndex(content)
		if len(loc) >= 4 {
			origTitle := content[loc[2]:loc[3]]
			cleaned := strings.TrimSpace(tokenTagRegex.ReplaceAllString(origTitle, ""))
			newTitle := fmt.Sprintf("%s %s", tokenTag, cleaned)
			if newTitle != origTitle {
				updatedContent := content[:loc[2]] + newTitle + content[loc[3]:]
				if err := os.WriteFile(pbtxtPath, []byte(updatedContent), 0644); err == nil {
					updated = true
				}
			}
		}
	}

	return updated
}

type toolCallArgs struct {
	Cwd        string `json:"Cwd"`
	TargetFile string `json:"TargetFile"`
}

type toolCallItem struct {
	Args toolCallArgs `json:"args"`
}

type stepWithTools struct {
	ToolCalls []toolCallItem `json:"tool_calls"`
}

// DetectProject inspects the initial transcript tool calls to classify the workspace/project.
func DetectProject(convID, brainDir string) string {
	tFile := filepath.Join(brainDir, convID, ".system_generated", "logs", "transcript.jsonl")
	f, err := os.Open(tFile)
	if err != nil {
		return "Unknown"
	}
	defer f.Close()

	var cwds []string
	scanner := bufio.NewScanner(f)
	linesRead := 0
	for scanner.Scan() && linesRead < 40 {
		linesRead++
		line := scanner.Bytes()
		var step stepWithTools
		if err := json.Unmarshal(line, &step); err == nil {
			for _, tc := range step.ToolCalls {
				if tc.Args.Cwd != "" {
					cwds = append(cwds, tc.Args.Cwd)
				}
				if tc.Args.TargetFile != "" {
					cwds = append(cwds, tc.Args.TargetFile)
				}
			}
		}
	}

	joined := strings.Join(cwds, " ")
	if strings.Contains(joined, "data_mine") {
		return "data_mine"
	} else if strings.Contains(joined, "treeclimber") || strings.Contains(joined, "tinrook") {
		return "tinrook (treeclimber)"
	} else if strings.Contains(joined, "nordiska-team1-1.worktrees") || strings.Contains(joined, "pr-74") {
		return "nordiska (pr-74 worktree)"
	} else if strings.Contains(joined, "nordiska-team1-1") {
		return "nordiska-team1-1"
	} else if strings.Contains(joined, "nordiska") {
		return "nordiska-team1"
	} else if strings.Contains(joined, "/home/") || strings.Contains(joined, "Users") {
		return "system / home"
	}

	return "general / other"
}
