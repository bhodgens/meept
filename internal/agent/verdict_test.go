package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerdictString(t *testing.T) {
	tests := []struct {
		name    string
		verdict Verdict
		want    string
	}{
		{"pass", VerdictPass, "PASS"},
		{"fail", VerdictFail, "FAIL"},
		{"partial", VerdictPartial, "PARTIAL"},
		{"unknown", VerdictUnknown, "UNKNOWN"},
		{"out of range", Verdict(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.verdict.String())
		})
	}
}

func TestParseVerdict(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		wantVerdict Verdict
		wantChecks  int
	}{
		{
			name:        "PASS verdict",
			output:      "CHECK: build\nCOMMAND: go build ./...\nOUTPUT: ok\nRESULT: PASS\n\nVERDICT: PASS",
			wantVerdict: VerdictPass,
			wantChecks:  1,
		},
		{
			name:        "FAIL verdict",
			output:      "CHECK: tests\nCOMMAND: go test ./...\nOUTPUT: FAIL\nRESULT: FAIL\n\nVERDICT: FAIL",
			wantVerdict: VerdictFail,
			wantChecks:  1,
		},
		{
			name:        "PARTIAL verdict",
			output:      "CHECK: edge\nCOMMAND: run edge\nOUTPUT: partial\nRESULT: PASS\n\nVERDICT: PARTIAL",
			wantVerdict: VerdictPartial,
			wantChecks:  1,
		},
		{
			name:        "malformed verdict",
			output:      "VERDICT: MAYBE",
			wantVerdict: VerdictUnknown,
			wantChecks:  0,
		},
		{
			name:        "empty output",
			output:      "",
			wantVerdict: VerdictUnknown,
			wantChecks:  0,
		},
		{
			name:        "no verdict line",
			output:      "CHECK: build\nCOMMAND: go build\nOUTPUT: ok\nRESULT: PASS",
			wantVerdict: VerdictUnknown,
			wantChecks:  1,
		},
		{
			name:        "verdict with leading whitespace on line",
			output:      "  VERDICT: PASS",
			wantVerdict: VerdictUnknown,
			wantChecks:  0,
		},
		{
			name:        "verdict with trailing whitespace",
			output:      "VERDICT: PASS   ",
			wantVerdict: VerdictPass,
			wantChecks:  0,
		},
		{
			name:        "multiple checks with verdict",
			output:      "CHECK: build\nCOMMAND: go build\nOUTPUT: ok\nRESULT: PASS\n\nCHECK: test\nCOMMAND: go test\nOUTPUT: ok\nRESULT: PASS\n\nVERDICT: PASS",
			wantVerdict: VerdictPass,
			wantChecks:  2,
		},
		{
			name:        "verdict mid-text not at line start",
			output:      "The VERDICT: PASS was given",
			wantVerdict: VerdictUnknown,
			wantChecks:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, checks := ParseVerdict(tt.output)
			assert.Equal(t, tt.wantVerdict, got)
			assert.Len(t, checks, tt.wantChecks)
		})
	}
}

func TestParseChecks(t *testing.T) {
	t.Run("single check with all fields", func(t *testing.T) {
		output := "CHECK: build compiles\nCOMMAND: go build ./...\nOUTPUT: exit 0\nRESULT: PASS"
		checks := parseChecks(output)
		require.Len(t, checks, 1)
		assert.Equal(t, "build compiles", checks[0].Name)
		assert.Equal(t, "go build ./...", checks[0].Command)
		assert.Equal(t, "exit 0", checks[0].Output)
		assert.True(t, checks[0].Passed)
	})

	t.Run("check with FAIL result", func(t *testing.T) {
		output := "CHECK: race detector\nCOMMAND: go test -race ./...\nOUTPUT: DATA RACE\nRESULT: FAIL"
		checks := parseChecks(output)
		require.Len(t, checks, 1)
		assert.False(t, checks[0].Passed)
	})

	t.Run("multiple checks", func(t *testing.T) {
		output := "CHECK: build\nCOMMAND: go build\nOUTPUT: ok\nRESULT: PASS\n\nCHECK: lint\nCOMMAND: golangci-lint run\nOUTPUT: 3 issues\nRESULT: FAIL\n\nCHECK: test\nCOMMAND: go test\nOUTPUT: ok\nRESULT: PASS"
		checks := parseChecks(output)
		require.Len(t, checks, 3)
		assert.Equal(t, "build", checks[0].Name)
		assert.True(t, checks[0].Passed)
		assert.Equal(t, "lint", checks[1].Name)
		assert.False(t, checks[1].Passed)
		assert.Equal(t, "test", checks[2].Name)
		assert.True(t, checks[2].Passed)
	})

	t.Run("no checks", func(t *testing.T) {
		checks := parseChecks("no checks here")
		assert.Nil(t, checks)
	})

	t.Run("check without optional fields", func(t *testing.T) {
		output := "CHECK: visual inspection"
		checks := parseChecks(output)
		require.Len(t, checks, 1)
		assert.Equal(t, "visual inspection", checks[0].Name)
		assert.Empty(t, checks[0].Command)
		assert.Empty(t, checks[0].Output)
		assert.False(t, checks[0].Passed)
	})
}
