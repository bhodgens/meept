package memory

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFreshnessTextRecent(t *testing.T) {
	tests := []struct {
		name    string
		ageDays int
	}{
		{"zero days", 0},
		{"one day", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MemoryFreshnessText(tt.ageDays)
			assert.Empty(t, result)
		})
	}
}

func TestFreshnessTextOld(t *testing.T) {
	result := MemoryFreshnessText(30)
	require.NotEmpty(t, result)
	assert.Contains(t, result, "30 days")
	assert.Contains(t, result, "point-in-time observations")
	assert.Contains(t, result, "Verify against current code")
}

func TestFreshnessTextBoundary(t *testing.T) {
	// Exactly 1 day should return empty (not stale yet).
	assert.Empty(t, MemoryFreshnessText(1))
	// 2 days should produce a caveat.
	result := MemoryFreshnessText(2)
	require.NotEmpty(t, result)
	assert.Contains(t, result, "2 days")
}

func TestFormatMemoryWithCaveat(t *testing.T) {
	old := time.Now().Add(-30 * 24 * time.Hour)
	content := "The function is at main.go:42"

	result := FormatMemoryWithFreshness(content, &old)

	require.NotEqual(t, content, result)
	assert.True(t, strings.HasPrefix(result, content))
	assert.Contains(t, result, "30 days")
	assert.Contains(t, result, "[")
	assert.Contains(t, result, "]")
}

func TestFormatMemoryWithoutCaveat(t *testing.T) {
	recent := time.Now().Add(-12 * time.Hour) // Less than 1 day.
	content := "The function is at main.go:42"

	result := FormatMemoryWithFreshness(content, &recent)
	assert.Equal(t, content, result)
}

func TestFormatMemoryNilUpdatedAt(t *testing.T) {
	content := "Some memory content"
	result := FormatMemoryWithFreshness(content, nil)
	assert.Equal(t, content, result)
}
