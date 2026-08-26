package security

import (
	"testing"

	pkgsecurity "github.com/caimlas/meept/pkg/security"
)

func TestLookupBaseRule_BrowserTools(t *testing.T) {
	e := newEngineWithFence(t, nil)

	cases := []struct {
		tool string
		want RiskLevel
	}{
		{"browser_navigate", RiskHigh},
		{"browser_click", RiskHigh},
		{"browser_type", RiskHigh},
		{"browser_read_text", RiskLow},
		{"browser_screenshot", RiskLow},
		{"browser_close", RiskLow},
		// Fail-closed unknowns under the prefix.
		{"browser_unknown_action", RiskHigh},
	}
	for _, tc := range cases {
		got, ok := e.lookupBaseRule(tc.tool, tc.tool)
		if !ok && got == 0 {
			t.Fatalf("lookupBaseRule(%s) returned not-ok", tc.tool)
		}
		if got != tc.want {
			t.Errorf("lookupBaseRule(%s) risk = %v, want %v", tc.tool, got, tc.want)
		}
	}

	// Non-browser names must NOT be claimed by the browser rule.
	if _, ok := pkgsecurity.BrowserToolRule("web_fetch"); ok {
		t.Error("BrowserToolRule should not claim web_fetch")
	}
	if _, ok := pkgsecurity.BrowserToolRule("file_read"); ok {
		t.Error("BrowserToolRule should not claim file_read")
	}
}
