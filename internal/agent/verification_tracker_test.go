package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordToolCall(t *testing.T) {
	tests := []struct {
		name         string
		toolName     string
		filePath     string
		wantCount    int
		wantFilesLen int
	}{
		{"file_write increments", "file_write", "main.go", 1, 1},
		{"file_edit increments", "file_edit", "util.go", 1, 1},
		{"file_delete increments", "file_delete", "old.go", 1, 1},
		{"git_commit increments", "git_commit", "", 1, 0},
		{"shell_execute increments", "shell_execute", "", 1, 0},
		{"file_read does not increment", "file_read", "main.go", 0, 0},
		{"web_search does not increment", "web_search", "", 0, 0},
		{"empty path not recorded", "file_write", "", 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewVerificationTracker(5)
			tr.RecordToolCall(tt.toolName, tt.filePath)
			assert.Equal(t, tt.wantCount, tr.editCount)
			assert.Len(t, tr.filesChanged, tt.wantFilesLen)
		})
	}
}

func TestShouldTrigger(t *testing.T) {
	tests := []struct {
		name      string
		threshold int
		edits     int
		want      bool
	}{
		{"below threshold", 3, 2, false},
		{"at threshold", 3, 3, true},
		{"above threshold", 3, 5, true},
		{"zero edits", 3, 0, false},
		{"threshold one fires on first edit", 1, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewVerificationTracker(tt.threshold)
			for i := 0; i < tt.edits; i++ {
				tr.RecordToolCall("file_write", "f.go")
			}
			assert.Equal(t, tt.want, tr.ShouldTrigger())
		})
	}
}

func TestSnapshot(t *testing.T) {
	tr := NewVerificationTracker(3)
	tr.RecordToolCall("file_write", "a.go")
	tr.RecordToolCall("file_edit", "b.go")
	tr.RecordToolCall("file_delete", "c.go")

	files := tr.Snapshot()
	require.Len(t, files, 3)
	assert.Equal(t, []string{"a.go", "b.go", "c.go"}, files)

	// After snapshot, state is reset.
	assert.Equal(t, 0, tr.editCount)
	assert.Empty(t, tr.filesChanged)
	assert.False(t, tr.ShouldTrigger())
}

func TestReset(t *testing.T) {
	tr := NewVerificationTracker(2)
	tr.RecordToolCall("file_write", "x.go")
	tr.RecordToolCall("file_edit", "y.go")
	require.True(t, tr.ShouldTrigger())

	tr.Reset()

	assert.Equal(t, 0, tr.editCount)
	assert.Empty(t, tr.filesChanged)
	assert.False(t, tr.ShouldTrigger())
}

func TestNewVerificationTrackerDefaultThreshold(t *testing.T) {
	tests := []struct {
		name      string
		input     int
		wantThresh int
	}{
		{"zero defaults to 3", 0, 3},
		{"negative defaults to 3", -1, 3},
		{"positive kept", 5, 5},
		{"one kept", 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewVerificationTracker(tt.input)
			assert.Equal(t, tt.wantThresh, tr.threshold)
		})
	}
}
