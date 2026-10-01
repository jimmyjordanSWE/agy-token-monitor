package config

import (
	"os"
	"path/filepath"
)

// Config holds cross-platform file paths and runtime settings.
type Config struct {
	BaseDir        string
	BrainDir       string
	RulesDir       string
	HandoffRulePath string
	DBPath         string
	SummariesDB    string
	AnnotationsDir string
}

// DefaultConfig resolves paths using cross-platform conventions.
// It respects ANTIGRAVITY_DIR or ANTIGRAVITY_HOME if set, otherwise falls back
// to ~/.gemini/antigravity (using os.UserHomeDir for Windows/macOS/Linux compatibility).
func DefaultConfig() (*Config, error) {
	baseDir := os.Getenv("ANTIGRAVITY_DIR")
	if baseDir == "" {
		baseDir = os.Getenv("ANTIGRAVITY_HOME")
	}
	if baseDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		baseDir = filepath.Join(homeDir, ".gemini", "antigravity")
	}

	brainDir := filepath.Join(baseDir, "brain")
	rulesDir := filepath.Join(baseDir, "rules")
	handoffRulePath := filepath.Join(rulesDir, "context_handoff.md")
	dbPath := filepath.Join(baseDir, "token_metrics.db")
	summariesDB := filepath.Join(baseDir, "conversation_summaries.db")
	annotationsDir := filepath.Join(baseDir, "annotations")

	return &Config{
		BaseDir:        baseDir,
		BrainDir:       brainDir,
		RulesDir:       rulesDir,
		HandoffRulePath: handoffRulePath,
		DBPath:         dbPath,
		SummariesDB:    summariesDB,
		AnnotationsDir: annotationsDir,
	}, nil
}
