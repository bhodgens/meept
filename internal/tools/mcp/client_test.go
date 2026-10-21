package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSanitizeResourceURI_BlocksFileScheme pins the file-URI blocking logic
// in the CallTool resource-render path (sanitizeResourceURI). The previous
// version of this file asserted only `assert.True(t, true, ...)` and passed
// with the blocking logic deleted — these cases fail against both the
// vacuous-assertion file and the pre-c9cf720a substring-only logic:
//
//		old logic: strings.HasPrefix(uri, "file://") && (contains "/etc/" ||
//		           contains "/../" || HasPrefix "file:///")
//
//	  - "FILE://x"     → old logic: HasPrefix fails (case) → NOT blocked (red)
//	  - " file://x"    → old logic AND untrimmed strings.Index scheme
//	    extraction: leading space defeats both → NOT blocked (red)
//	  - "file:/secret" → old logic: no "//" → NOT blocked (red)
func TestSanitizeResourceURI_BlocksFileScheme(t *testing.T) {
	blocked := []string{
		"file://etc/passwd",
		"FILE://x",
		" file://x",    // leading space: scheme extraction must TrimSpace first
		"file:/secret", // single-slash form
		"file:///etc/shadow",
		"FiLe://mixed/case",
		"\tfile://tab-prefixed",
	}
	for _, uri := range blocked {
		t.Run("blocked/"+uri, func(t *testing.T) {
			got := sanitizeResourceURI(uri)
			assert.Equal(t, blockedFileURI, got, "file-scheme URI %q must be blocked", uri)
		})
	}
}

// TestSanitizeResourceURI_BenignPassesThrough: non-file schemes are rendered
// verbatim (display-only surface; no fetch path).
func TestSanitizeResourceURI_BenignPassesThrough(t *testing.T) {
	benign := []string{
		"https://example.com/doc",
		"http://internal.host/path",
		"mcp://resource/1",
		"ftp://files.example.com/x",
		"not-a-uri",
	}
	for _, uri := range benign {
		t.Run("passthrough/"+uri, func(t *testing.T) {
			got := sanitizeResourceURI(uri)
			assert.Equal(t, uri, got, "benign URI %q must pass through unchanged", uri)
		})
	}
}

// TestSanitizeResourceURI_EdgeShapes: no-colon and colon-first inputs never
// crash and never block (empty scheme is not "file").
func TestSanitizeResourceURI_EdgeShapes(t *testing.T) {
	assert.Equal(t, "", sanitizeResourceURI(""))
	assert.Equal(t, ":file://x", sanitizeResourceURI(":file://x")) // idx==0 → not a scheme
	assert.Equal(t, "filesystem://x", sanitizeResourceURI("filesystem://x"))
}
