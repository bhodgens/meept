package tui

import (
	"fmt"
	"testing"
)

func TestRenderIntakeStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   IntakeStatus
		err      error
		expected string
	}{
		{
			name: "full enabled JSON",
			status: IntakeStatus{
				Enabled:         true,
				WatchRepo:       "owner/repo",
				PollInterval:    "1h",
				RequiredLabels:  []string{"bug", "feature"},
				DoneLabel:       "done",
				DefaultEmployee: "bob",
				GateCommand:     "go test",
				ProjectNumber:   123,
				PollerScheduled: true,
			},
			expected: "intake status\n---------------\n  enabled:      true\n  watch_repo:    owner/repo\n  interval:      1h\n  labels:       bug, feature\n  done_label:   done\n  employee:     bob\n  gate:         go test\n  project:      #123\n  poll job:     scheduled (intake_poller)\n",
		},
		{
			name: "disabled",
			status: IntakeStatus{
				Enabled: false,
				Note:    "intake disabled",
			},
			expected: "intake status\n---------------\n  status: intake disabled",
		},
		{
			name: "empty required_labels",
			status: IntakeStatus{
				Enabled:         true,
				WatchRepo:       "r",
				PollInterval:    "1h",
				DoneLabel:       "d",
				DefaultEmployee: "e",
				GateCommand:     "g",
				ProjectNumber:   1,
			},
			expected: "intake status\n---------------\n  enabled:      true\n  watch_repo:    r\n  interval:      1h\n  labels:       factory (default)\n  done_label:   d\n  employee:     e\n  gate:         g\n  project:      #1\n  poll job:     not scheduled\n",
		},
		{
			name:     "malformed input (err)",
			err:      fmt.Errorf("failed to fetch"),
			expected: "intake error: failed to fetch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderIntakeStatus(tt.status, tt.err)
			if got != tt.expected {
				t.Errorf("renderIntakeStatus() got = %q, want %q", got, tt.expected)
			}
		})
	}
}
