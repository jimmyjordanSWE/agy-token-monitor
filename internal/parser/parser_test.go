package parser

import (
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		chars    int
		expected int64
	}{
		{0, 0},
		{1, 1},
		{4, 1},
		{38, 10},
		{3800, 1000},
	}

	for _, c := range cases {
		got := EstimateTokens(c.chars)
		if got != c.expected {
			t.Errorf("EstimateTokens(%d) = %d, expected %d", c.chars, got, c.expected)
		}
	}
}

func TestFormatTokenTag(t *testing.T) {
	cases := []struct {
		tokens   int64
		expected string
	}{
		{0, "[0k]"},
		{500, "[500]"},
		{1500, "[2k]"},
		{150000, "[150k]"},
		{1250000, "[1.2M]"},
	}

	for _, c := range cases {
		got := FormatTokenTag(c.tokens)
		if got != c.expected {
			t.Errorf("FormatTokenTag(%d) = %q, expected %q", c.tokens, got, c.expected)
		}
	}
}

func TestParseStep(t *testing.T) {
	line := []byte(`{"step_index": 5, "source": "USER_EXPLICIT", "type": "USER_INPUT", "status": "DONE", "created_at": "2026-10-01T12:00:00Z", "content": "hello world", "thinking": "", "tool_calls": null}`)
	step, err := ParseStep(line)
	if err != nil {
		t.Fatalf("ParseStep failed: %v", err)
	}

	if step.StepIndex != 5 {
		t.Errorf("expected step_index 5, got %d", step.StepIndex)
	}
	if step.Source != "USER_EXPLICIT" {
		t.Errorf("expected source USER_EXPLICIT, got %s", step.Source)
	}
	if step.ContentChars != 11 {
		t.Errorf("expected content_chars 11, got %d", step.ContentChars)
	}
}
