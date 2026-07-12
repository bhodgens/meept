package learning

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeFeedback(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"positive": FeedbackPositive,
		"POS":      FeedbackPositive,
		"+1":       FeedbackPositive,
		"negative": FeedbackNegative,
		"bad":      FeedbackNegative,
		"neutral":  FeedbackNeutral,
		"clear":    FeedbackNeutral,
	}
	for in, want := range cases {
		got, ok := NormalizeFeedback(in)
		if !ok || got != want {
			t.Errorf("NormalizeFeedback(%q) = %q,%v want %q,true", in, got, ok, want)
		}
	}
	if _, ok := NormalizeFeedback("meh"); ok {
		t.Error("expected invalid feedback to fail")
	}
}

func TestApplyUserFeedback(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	traj := ResearchTrajectory{
		ID:        "ltraj-1",
		SessionID: "sess-1",
		Domain:    "code",
		Intent:    "how to parse json",
		Query:     "how to parse json",
		Synthesis: "use encoding/json",
		ToolCalls: []ToolCallRecord{{Tool: "file_read", Used: true}},
		TaskOutcome: TaskOutcome{
			Success: true,
		},
		Timestamp: time.Now().UTC(),
	}
	line, _ := json.Marshal(traj)
	if err := os.WriteFile(filepath.Join(tmp, "raw_captures.jsonl"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	datasetsDir := filepath.Join(tmp, "datasets")
	if err := os.MkdirAll(datasetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ex := TrainingExample{
		Instruction: "how to parse json",
		Output:      "use encoding/json",
		Metadata: ExampleMetadata{
			Domain:       "code",
			SessionID:    "sess-1",
			QualityScore: 0.85,
		},
	}
	exLine, _ := json.Marshal(ex)
	if err := os.WriteFile(filepath.Join(datasetsDir, "code.jsonl"), append(exLine, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := ApplyUserFeedback(tmp, "sess-1", "", "positive")
	if err != nil {
		t.Fatalf("ApplyUserFeedback: %v", err)
	}
	if res.Matched != 1 || res.Updated != 1 {
		t.Fatalf("result = %+v, want matched=1 updated=1", res)
	}

	data, err := os.ReadFile(filepath.Join(tmp, "raw_captures.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var got ResearchTrajectory
	if err := json.Unmarshal(data[:len(data)-1], &got); err != nil {
		t.Fatal(err)
	}
	if got.TaskOutcome.UserFeedback != FeedbackPositive {
		t.Errorf("feedback = %q, want positive", got.TaskOutcome.UserFeedback)
	}

	// success 0.2 + used 0.15 + positive 0.15 + base 0.5 = 1.0
	dsData, err := os.ReadFile(filepath.Join(datasetsDir, "code.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var gotEx TrainingExample
	if err := json.Unmarshal(dsData[:len(dsData)-1], &gotEx); err != nil {
		t.Fatal(err)
	}
	if gotEx.Metadata.QualityScore < 0.99 {
		t.Errorf("dataset quality = %f, want ~1.0 after positive feedback", gotEx.Metadata.QualityScore)
	}

	if _, err := ApplyUserFeedback(tmp, "sess-1", "ltraj-1", "negative"); err != nil {
		t.Fatal(err)
	}
	// success+used+negative: 0.5+0.2+0.15-0.2 = 0.65
	dsData, _ = os.ReadFile(filepath.Join(datasetsDir, "code.jsonl"))
	if err := json.Unmarshal(dsData[:len(dsData)-1], &gotEx); err != nil {
		t.Fatal(err)
	}
	if gotEx.Metadata.QualityScore < 0.64 || gotEx.Metadata.QualityScore > 0.66 {
		t.Errorf("dataset quality after negative = %f, want ~0.65", gotEx.Metadata.QualityScore)
	}
}

func TestScoreExampleNegativeFeedback(t *testing.T) {
	t.Parallel()
	traj := ResearchTrajectory{
		TaskOutcome: TaskOutcome{Success: true, UserFeedback: FeedbackNegative},
		ToolCalls:   []ToolCallRecord{{Used: true}},
	}
	got := ScoreExample(traj)
	if got < 0.64 || got > 0.66 {
		t.Errorf("score = %f, want ~0.65", got)
	}
}
