package q

import (
	"log/slog"
	"testing"
	"time"
)

var testLogger = slog.Default()

func makeSession(sessionID string, agentID string, intents []string, duration time.Duration,
	iterations, revisions, errors, switches int, outcome string, difficulty float64, anomalyFlags []string,
) *SessionAnalysis {
	sa := &SessionAnalysis{
		SessionID:      sessionID,
		AgentID:        agentID,
		Intents:        intents,
		Duration:       duration,
		IterationCount: iterations,
		RevisionCycles: revisions,
		TokenUsage:     iterations * 500,
		Outcome:        outcome,
		DifficultyScore: difficulty,
		AnomalyFlags:   anomalyFlags,
		StartTime:      time.Now().Add(-duration),
		EndTime:        time.Now(),
	}
	// Add tool call records for testing tool failure detection
	sa.ToolCalls = []ToolCallRecord{
		{ToolName: "file_read", Success: errors == 0},
		{ToolName: "shell_execute", Success: errors == 0},
	}
	return sa
}

func makeSessionsWithToolFailureCount(n int, failRate float64) []*SessionAnalysis {
	sessions := make([]*SessionAnalysis, 0, n)
	for i := 0; i < n; i++ {
		success := true
		if float64(i) < float64(n)*failRate {
			success = false
		}
		sa := &SessionAnalysis{
			SessionID:      "session-" + string(rune('a'+i)),
			AgentID:        "chat",
			Intents:        []string{"debug"},
			Duration:       10 * time.Minute,
			IterationCount: 5,
			RevisionCycles: 0,
			ToolCalls: []ToolCallRecord{
				{ToolName: "shell_execute", Success: success},
			},
			Outcome:       "completed",
			DifficultyScore: 0.3,
			StartTime:     time.Now().Add(-10 * time.Minute),
			EndTime:       time.Now(),
		}
		sessions = append(sessions, sa)
	}
	return sessions
}

func TestPatternDetectorDetectModelMisconfiguration(t *testing.T) {
	tests := []struct {
		name            string
		sessions        []*SessionAnalysis
		needPattern     bool
		threshold       float64
		minSessions     int
	}{
		{
			name: "no pattern - low variance",
			sessions: []*SessionAnalysis{
				makeSession("s1", "chat", []string{"debug"}, 10*time.Minute, 5, 0, 0, 0, "completed", 0.3, nil),
				makeSession("s2", "chat", []string{"debug"}, 11*time.Minute, 5, 0, 0, 0, "completed", 0.3, nil),
				makeSession("s3", "chat", []string{"debug"}, 9*time.Minute, 4, 0, 0, 0, "completed", 0.2, nil),
			},
			needPattern: false,
			threshold:   3.0,
			minSessions: 3,
		},
		{
			name: "high variance - pattern detected",
			sessions: []*SessionAnalysis{
				makeSession("s1", "chat", []string{"debug"}, 5*time.Minute, 3, 0, 0, 0, "completed", 0.2, nil),
				makeSession("s2", "chat", []string{"debug"}, 120*time.Minute, 30, 10, 5, 3, "failed", 0.9, []string{"long_duration", "high_iterations", "high_revisions"}),
				makeSession("s3", "chat", []string{"debug"}, 8*time.Minute, 4, 0, 0, 0, "completed", 0.3, nil),
			},
			needPattern: true,
			threshold:   3.0,
			minSessions: 3,
		},
		{
			name: "fewer than 3 sessions - no pattern",
			sessions: []*SessionAnalysis{
				makeSession("s1", "chat", []string{"debug"}, 5*time.Minute, 3, 0, 0, 0, "completed", 0.2, nil),
				makeSession("s2", "chat", []string{"debug"}, 120*time.Minute, 30, 10, 5, 3, "failed", 0.9, nil),
			},
			needPattern: false,
			threshold:   3.0,
			minSessions: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewPatternDetector(testLogger, PatternDetectorConfig{
				MinSessionsForPattern:      tt.minSessions,
				HighErrorRateThreshold:     0.2,
				HighRejectionRateThreshold: 0.3,
				DurationVarianceThreshold:  tt.threshold,
			})

			report := detector.DetectPatterns(tt.sessions)

			patternsFound := 0
			for _, r := range report {
				if r.PatternType == "model_misconfiguration" {
					patternsFound++
				}
			}

			if tt.needPattern && patternsFound == 0 {
				t.Errorf("expected model_misconfiguration pattern, got %d", patternsFound)
			}
			if !tt.needPattern && patternsFound > 0 {
				t.Errorf("unexpected model_misconfiguration pattern, got %d", patternsFound)
			}
		})
	}
}

func TestPatternDetectorDetectHighErrorRate(t *testing.T) {
	tests := []struct {
		name        string
		sessions    []*SessionAnalysis
		needPattern bool
		minSessions int
	}{
		{
			name: "low error rate - no pattern",
			sessions: []*SessionAnalysis{
				makeSession("s1", "coder", []string{"code"}, 10*time.Minute, 5, 0, 0, 0, "completed", 0.3, nil),
				makeSession("s2", "coder", []string{"code"}, 12*time.Minute, 6, 0, 0, 0, "completed", 0.4, nil),
				makeSession("s3", "coder", []string{"code"}, 11*time.Minute, 5, 0, 0, 0, "completed", 0.3, nil),
				makeSession("s4", "coder", []string{"code"}, 15*time.Minute, 8, 0, 0, 0, "completed", 0.5, nil),
				makeSession("s5", "coder", []string{"code"}, 10*time.Minute, 5, 0, 0, 0, "completed", 0.3, nil),
			},
			needPattern: false,
			minSessions: 3,
		},
		{
			name: "high error rate - pattern detected",
			sessions: []*SessionAnalysis{
				makeSession("s1", "coder", []string{"code"}, 10*time.Minute, 5, 6, 0, 0, "completed", 0.3, []string{"high_revisions"}),
				makeSession("s2", "coder", []string{"code"}, 12*time.Minute, 6, 7, 0, 0, "completed", 0.4, []string{"high_revisions"}),
				makeSession("s3", "coder", []string{"code"}, 11*time.Minute, 5, 8, 0, 0, "failed", 0.7, []string{"high_revisions", "high_iterations"}),
				makeSession("s4", "coder", []string{"code"}, 15*time.Minute, 8, 5, 0, 0, "completed", 0.5, nil),
				makeSession("s5", "coder", []string{"code"}, 10*time.Minute, 5, 6, 0, 0, "completed", 0.3, []string{"high_revisions"}),
			},
			needPattern: true,
			minSessions: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewPatternDetector(testLogger, PatternDetectorConfig{
				MinSessionsForPattern:      tt.minSessions,
				HighErrorRateThreshold:     0.2,
				HighRejectionRateThreshold: 0.3,
				DurationVarianceThreshold:  3.0,
			})

			report := detector.DetectPatterns(tt.sessions)

			patternsFound := 0
			for _, r := range report {
				if r.PatternType == "high_error_rate" {
					patternsFound++
					if r.Confidence < 0.5 {
						t.Errorf("high_error_rate confidence %.2f < 0.5", r.Confidence)
					}
					if r.AffectedAgent != "coder" {
						t.Errorf("expected affected agent 'coder', got %q", r.AffectedAgent)
					}
				}
			}

			if tt.needPattern && patternsFound == 0 {
				t.Error("expected high_error_rate pattern, none found")
			}
			if !tt.needPattern && patternsFound > 0 {
				t.Errorf("unexpected high_error_rate pattern, got %d", patternsFound)
			}
		})
	}
}

func TestPatternDetectorDetectWrongAgentAssignment(t *testing.T) {
	tests := []struct {
		name        string
		sessions    []*SessionAnalysis
		needPattern bool
		minSessions int
	}{
		{
			name: "single agent handles well - no pattern",
			sessions: []*SessionAnalysis{
				makeSession("s1", "coder", []string{"coding"}, 10*time.Minute, 5, 1, 0, 0, "completed", 0.3, nil),
				makeSession("s2", "coder", []string{"coding"}, 12*time.Minute, 6, 2, 0, 0, "completed", 0.4, nil),
				makeSession("s3", "coder", []string{"coding"}, 11*time.Minute, 5, 1, 0, 0, "completed", 0.3, nil),
			},
			needPattern: false,
			minSessions: 3,
		},
		{
			name: "multiple agents struggle - pattern detected",
			sessions: []*SessionAnalysis{
				makeSession("s1", "coder", []string{"debug"}, 20*time.Minute, 15, 5, 2, 1, "failed", 0.7, nil),
				makeSession("s2", "chat", []string{"debug"}, 25*time.Minute, 18, 6, 3, 2, "failed", 0.8, nil),
				makeSession("s3", "analyst", []string{"debug"}, 30*time.Minute, 20, 7, 2, 1, "failed", 0.75, nil),
			},
			needPattern: true,
			minSessions: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewPatternDetector(testLogger, PatternDetectorConfig{
				MinSessionsForPattern:      tt.minSessions,
				HighErrorRateThreshold:     0.2,
				HighRejectionRateThreshold: 0.3,
				DurationVarianceThreshold:  3.0,
			})

			report := detector.DetectPatterns(tt.sessions)

			patternsFound := 0
			for _, r := range report {
				if r.PatternType == "wrong_agent_assignment" {
					patternsFound++
					if r.Confidence > 1.0 {
						t.Errorf("wrong_agent_assignment confidence %.2f > 1.0", r.Confidence)
					}
					if r.MisconfigurationType != "capability_gap" {
						t.Errorf("expected misconfiguration_type 'capability_gap', got %q", r.MisconfigurationType)
					}
				}
			}

			if tt.needPattern && patternsFound == 0 {
				t.Error("expected wrong_agent_assignment pattern, none found")
			}
		})
	}
}

func TestPatternDetectorDetectHighToolFailureRate(t *testing.T) {
	tests := []struct {
		name         string
		totalCalls   int
		failRatio    float64
		needPattern  bool
		minTotal     int
	}{
		{
			name:        "low failure rate - no pattern",
			totalCalls:  10,
			failRatio:   0.1,
			needPattern: false,
		},
		{
			name:        "high failure rate - pattern detected",
			totalCalls:  10,
			failRatio:   0.5,
			needPattern: true,
		},
		{
			name:        "too few calls - no pattern even with high failure",
			totalCalls:  3,
			failRatio:   1.0,
			needPattern: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessions := makeSessionsWithToolFailureCount(tt.totalCalls, tt.failRatio)

			detector := NewPatternDetector(testLogger, PatternDetectorConfig{
				MinSessionsForPattern:      3,
				HighErrorRateThreshold:     0.2,
				HighRejectionRateThreshold: 0.3,
				DurationVarianceThreshold:  3.0,
			})

			report := detector.DetectPatterns(sessions)

			patternsFound := 0
			for _, r := range report {
				if r.PatternType == "high_tool_failure_rate" {
					patternsFound++
					if r.MetricBaseline != 0.2 {
						t.Errorf("expected baseline 0.2, got %.2f", r.MetricBaseline)
					}
					if r.MisconfigurationType != "tool_deficiency" {
						t.Errorf("expected misconfiguration_type 'tool_deficiency', got %q", r.MisconfigurationType)
					}
				}
			}

			if tt.needPattern && patternsFound == 0 {
				t.Error("expected high_tool_failure_rate pattern, none found")
			}
		})
	}
}

func TestPatternDetectorDetectHighRejectionRate(t *testing.T) {
	tests := []struct {
		name        string
		sessions    []*SessionAnalysis
		needPattern bool
		minSessions int
	}{
		{
			name: "low rejection rate - no pattern",
			sessions: []*SessionAnalysis{
				makeSession("s1", "coder", []string{"code"}, 10*time.Minute, 5, 1, 0, 0, "completed", 0.3, nil),
				makeSession("s2", "coder", []string{"code"}, 12*time.Minute, 6, 0, 0, 0, "completed", 0.4, nil),
				makeSession("s3", "coder", []string{"code"}, 11*time.Minute, 5, 1, 0, 0, "completed", 0.3, nil),
			},
			needPattern: false,
			minSessions: 3,
		},
		{
			name: "high rejection rate - pattern detected",
			sessions: []*SessionAnalysis{
				makeSession("s1", "coder", []string{"code"}, 10*time.Minute, 5, 3, 0, 0, "completed", 0.5, nil),
				makeSession("s2", "coder", []string{"code"}, 12*time.Minute, 6, 4, 0, 0, "completed", 0.6, nil),
				makeSession("s3", "coder", []string{"code"}, 11*time.Minute, 5, 3, 0, 0, "completed", 0.5, nil),
			},
			needPattern: true,
			minSessions: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewPatternDetector(testLogger, PatternDetectorConfig{
				MinSessionsForPattern:      tt.minSessions,
				HighErrorRateThreshold:     0.2,
				HighRejectionRateThreshold: 0.3,
				DurationVarianceThreshold:  3.0,
			})

			report := detector.DetectPatterns(tt.sessions)

			patternsFound := 0
			for _, r := range report {
				if r.PatternType == "high_rejection_rate" {
					patternsFound++
				}
			}

			if tt.needPattern && patternsFound == 0 {
				t.Error("expected high_rejection_rate pattern, none found")
			}
		})
	}
}

func TestPatternDetectorDetectRepeatedFailure(t *testing.T) {
	tests := []struct {
		name        string
		sessions    []*SessionAnalysis
		needPattern bool
	}{
		{
			name: "not enough sessions - no pattern",
			sessions: []*SessionAnalysis{
				makeSession("s1", "coder", []string{"debug"}, 20*time.Minute, 15, 5, 2, 1, "failed", 0.8, nil),
				makeSession("s2", "coder", []string{"debug"}, 25*time.Minute, 18, 6, 3, 2, "failed", 0.9, nil),
			},
			needPattern: false,
		},
		{
			name: "three failures same intent/agent - pattern",
			sessions: []*SessionAnalysis{
				makeSession("s1", "coder", []string{"debug"}, 20*time.Minute, 15, 5, 2, 1, "failed", 0.8, nil),
				makeSession("s2", "coder", []string{"debug"}, 25*time.Minute, 18, 6, 3, 2, "failed", 0.9, nil),
				makeSession("s3", "coder", []string{"debug"}, 22*time.Minute, 16, 5, 2, 1, "failed", 0.85, nil),
			},
			needPattern: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewPatternDetector(testLogger, PatternDetectorConfig{
				MinSessionsForPattern:      3,
				HighErrorRateThreshold:     0.2,
				HighRejectionRateThreshold: 0.3,
				DurationVarianceThreshold:  3.0,
			})

			report := detector.DetectPatterns(tt.sessions)

			patternsFound := 0
			for _, r := range report {
				if r.PatternType == "repeated_failure" {
					patternsFound++
				}
			}

			if tt.needPattern && patternsFound == 0 {
				t.Error("expected repeated_failure pattern, none found")
			}
		})
	}
}

func TestPatternDetectorFilterReportsConfidence(t *testing.T) {
	// Test that filterReports rejects reports with confidence < 0.5
	// Construct a pattern with low confidence (no error flags, so error rate = 0)
	sessions := []*SessionAnalysis{
		makeSession("s1", "coder", []string{"code"}, 10*time.Minute, 5, 0, 0, 0, "completed", 0.2, nil),
	}

	detector := NewPatternDetector(testLogger, PatternDetectorConfig{
		MinSessionsForPattern:      1,
		HighErrorRateThreshold:     0.2,
		HighRejectionRateThreshold: 0.3,
		DurationVarianceThreshold:  3.0,
	})

	report := detector.DetectPatterns(sessions)

	for _, r := range report {
		if r.Confidence < 0.5 {
			t.Errorf("filterReports should reject confidence %.2f < 0.5", r.Confidence)
		}
		if r.SessionCount < 1 {
			t.Errorf("filterReports should reject sessionCount %d < 1", r.SessionCount)
		}
	}
}

func TestPatternDetectorEmptyInput(t *testing.T) {
	detector := NewPatternDetector(testLogger, PatternDetectorConfig{
		MinSessionsForPattern:      3,
		HighErrorRateThreshold:     0.2,
		HighRejectionRateThreshold: 0.3,
		DurationVarianceThreshold:  3.0,
	})

	report := detector.DetectPatterns(nil)
	if len(report) != 0 {
		t.Errorf("DetectPatterns(nil) returned %d patterns, wanted 0", len(report))
	}
}

func TestPatternDetectorSessionWithNoAgentID(t *testing.T) {
	sessions := []*SessionAnalysis{
		{
			SessionID:      "s1",
			AgentID:        "",
			Intents:        []string{"debug"},
			Duration:       10 * time.Minute,
			IterationCount: 5,
			Outcome:        "completed",
			DifficultyScore: 0.3,
		},
	}

	detector := NewPatternDetector(testLogger, PatternDetectorConfig{
		MinSessionsForPattern:      1,
		HighErrorRateThreshold:     0.2,
		HighRejectionRateThreshold: 0.3,
		DurationVarianceThreshold:  3.0,
	})

	report := detector.DetectPatterns(sessions)
	for _, r := range report {
		if r.AffectedAgent == "" {
			t.Errorf("pattern report has empty AffectedAgent: %s", r.ID)
		}
	}
}

func TestPatternDetectorSortByConfidence(t *testing.T) {
	// Create sessions that trigger multiple patterns and verify sorting
	sessions := []*SessionAnalysis{
		makeSession("s1", "coder", []string{"debug", "code"}, 20*time.Minute, 15, 6, 2, 1, "failed", 0.8, []string{"high_revisions"}),
		makeSession("s2", "coder", []string{"debug", "code"}, 25*time.Minute, 18, 7, 3, 2, "failed", 0.9, []string{"high_revisions", "high_iterations"}),
		makeSession("s3", "coder", []string{"debug", "code"}, 22*time.Minute, 16, 5, 2, 1, "failed", 0.85, []string{"high_revisions"}),
		makeSession("s4", "chat", []string{"debug"}, 30*time.Minute, 20, 7, 2, 1, "failed", 0.75, []string{"high_iterations"}),
	}

	detector := NewPatternDetector(testLogger, PatternDetectorConfig{
		MinSessionsForPattern:      3,
		HighErrorRateThreshold:     0.2,
		HighRejectionRateThreshold: 0.3,
		DurationVarianceThreshold:  3.0,
	})

	report := detector.DetectPatterns(sessions)

	// Should be sorted by confidence descending
	for i := 1; i < len(report); i++ {
		if report[i].Confidence > report[i-1].Confidence {
			t.Errorf("reports not sorted by confidence descending: [%d]=%.2f < [%d]=%.2f",
				i-1, report[i-1].Confidence, i, report[i].Confidence)
		}
	}
}

func TestPatternDetectorEvidenceFields(t *testing.T) {
	sessions := []*SessionAnalysis{
		makeSession("s1", "coder", []string{"debug"}, 5*time.Minute, 3, 0, 0, 0, "completed", 0.2, nil),
		makeSession("s2", "coder", []string{"debug"}, 120*time.Minute, 30, 10, 5, 3, "failed", 0.9, []string{"long_duration", "high_iterations", "high_revisions"}),
		makeSession("s3", "coder", []string{"debug"}, 8*time.Minute, 4, 0, 0, 0, "completed", 0.3, nil),
	}

	detector := NewPatternDetector(testLogger, PatternDetectorConfig{
		MinSessionsForPattern:      3,
		HighErrorRateThreshold:     0.2,
		HighRejectionRateThreshold: 0.3,
		DurationVarianceThreshold:  3.0,
	})

	report := detector.DetectPatterns(sessions)

	for _, r := range report {
		// All reports should have a non-empty ID
		if r.ID == "" {
			t.Errorf("pattern report has empty ID, type=%s", r.PatternType)
		}
		// Session count should equal the number of input sessions
		if r.SessionCount != 3 {
			t.Errorf("pattern %s SessionCount=%d, want 3", r.PatternType, r.SessionCount)
		}
	}
}

func TestPatternDetectorConfidenceBounds(t *testing.T) {
	// Test that all pattern detections clamp confidence to [0.0, 1.0]
	sessions := []*SessionAnalysis{
		makeSession("s1", "coder", []string{"debug", "code"}, 5*time.Minute, 3, 6, 0, 0, "completed", 0.3, []string{"high_revisions"}),
		makeSession("s2", "coder", []string{"debug", "code"}, 8*time.Minute, 4, 6, 0, 0, "completed", 0.4, []string{"high_revisions"}),
		makeSession("s3", "coder", []string{"debug", "code"}, 10*time.Minute, 5, 6, 0, 0, "completed", 0.5, []string{"high_revisions"}),
	}

	detector := NewPatternDetector(testLogger, PatternDetectorConfig{
		MinSessionsForPattern:      3,
		HighErrorRateThreshold:     0.2,
		HighRejectionRateThreshold: 0.3,
		DurationVarianceThreshold:  3.0,
	})

	report := detector.DetectPatterns(sessions)
	for _, r := range report {
		if r.Confidence < 0.0 || r.Confidence > 1.0 {
			t.Errorf("confidence out of bounds [0,1]: %.2f for pattern %s", r.Confidence, r.PatternType)
		}
	}
}

func TestPatternDetectorDetectSkillOpportunity(t *testing.T) {
	tests := []struct {
		name        string
		sessions    []*SessionAnalysis
		needPattern bool
	}{
		{
			name: "few sessions - no pattern",
			sessions: []*SessionAnalysis{
				{
					SessionID: "s1",
					AgentID:   "coder",
					Intents:   []string{"build"},
					ToolCalls: []ToolCallRecord{
						{ToolName: "shell_execute", Success: true},
					},
				},
				{
					SessionID: "s2",
					AgentID:   "coder",
					Intents:   []string{"build"},
					ToolCalls: []ToolCallRecord{
						{ToolName: "shell_execute", Success: true},
					},
				},
			},
			needPattern: false,
		},
		{
			name: "repeated tool pattern - skill opportunity detected",
			sessions: []*SessionAnalysis{
				{
					SessionID: "s1",
					AgentID:   "coder",
					Intents:   []string{"build"},
					ToolCalls: []ToolCallRecord{
						{ToolName: "shell_execute", Success: true},
						{ToolName: "file_read", Success: true},
					},
				},
				{
					SessionID: "s2",
					AgentID:   "coder",
					Intents:   []string{"build"},
					ToolCalls: []ToolCallRecord{
						{ToolName: "shell_execute", Success: true},
						{ToolName: "file_read", Success: true},
					},
				},
				{
					SessionID: "s3",
					AgentID:   "coder",
					Intents:   []string{"build"},
					ToolCalls: []ToolCallRecord{
						{ToolName: "shell_execute", Success: true},
						{ToolName: "file_read", Success: true},
					},
				},
				{
					SessionID: "s4",
					AgentID:   "coder",
					Intents:   []string{"build"},
					ToolCalls: []ToolCallRecord{
						{ToolName: "shell_execute", Success: true},
						{ToolName: "file_read", Success: true},
					},
				},
				{
					SessionID: "s5",
					AgentID:   "coder",
					Intents:   []string{"build"},
					ToolCalls: []ToolCallRecord{
						{ToolName: "shell_execute", Success: true},
						{ToolName: "file_read", Success: true},
					},
				},
			},
			needPattern: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewPatternDetector(testLogger, PatternDetectorConfig{
				MinSessionsForPattern:      3,
				HighErrorRateThreshold:     0.2,
				HighRejectionRateThreshold: 0.3,
				DurationVarianceThreshold:  3.0,
			})

			report := detector.DetectPatterns(tt.sessions)

			patternsFound := 0
			for _, r := range report {
				if r.PatternType == "skill_opportunity" {
					patternsFound++
					if r.RecommendedAction != "add_skill" {
						t.Errorf("skill_opportunity should recommend 'add_skill', got %q", r.RecommendedAction)
					}
					if r.MisconfigurationType != "automation_opportunity" {
						t.Errorf("expected misconfiguration_type 'automation_opportunity', got %q", r.MisconfigurationType)
					}
				}
			}

			if tt.needPattern && patternsFound == 0 {
				t.Error("expected skill_opportunity pattern, none found")
			}
			if !tt.needPattern && patternsFound > 0 {
				t.Errorf("unexpected skill_opportunity pattern, got %d", patternsFound)
			}
		})
	}
}
