// Package theme holds the unified meept color tokens shared by the TUI and
// GUI. tokens.json5 is the single source of truth; consumers parse it at
// runtime (Go via this embed, Flutter via a test-guarded const copy).
package theme

import (
	_ "embed"
)

//go:embed tokens.json5
var TokensJSON5 []byte
