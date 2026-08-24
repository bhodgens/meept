package theme

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/tailscale/hujson"
)

// FrozenRoles is the exact role set every variant must define. Both the Go
// and Flutter consumers are held to this list by tests.
var FrozenRoles = []string{
	"primary", "primaryBright", "primaryDark", "primaryGlow", "accent",
	"secondary", "success", "warning", "error", "info",
	"background", "surface", "surfaceAlt", "border",
	"textPrimary", "textMuted", "terminalGreen", "terminalAmber",
}

// FrozenVariants is the shipped variant set.
var FrozenVariants = []string{"cyberpunk", "midnight", "solarized"}

var hexRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Tokens maps variant name → role → hex string.
type Tokens map[string]map[string]string

// Parse decodes tokens.json5 content (JSON5 with comments) into Tokens and
// validates structure: every frozen variant present, every variant defines
// exactly the frozen roles, all values are #RRGGBB hex.
func Parse(data []byte) (Tokens, error) {
	std, err := hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("theme: invalid json5: %w", err)
	}
	var t Tokens
	if err := json.Unmarshal(std, &t); err != nil {
		return nil, fmt.Errorf("theme: decode: %w", err)
	}
	want := make(map[string]bool, len(FrozenRoles))
	for _, r := range FrozenRoles {
		want[r] = true
	}
	for _, v := range FrozenVariants {
		roles, ok := t[v]
		if !ok {
			return nil, fmt.Errorf("theme: missing variant %q", v)
		}
		if len(roles) != len(FrozenRoles) {
			return nil, fmt.Errorf("theme: variant %q has %d roles, want %d", v, len(roles), len(FrozenRoles))
		}
		for r, hex := range roles {
			if !want[r] {
				return nil, fmt.Errorf("theme: variant %q has unknown role %q", v, r)
			}
			if !hexRe.MatchString(hex) {
				return nil, fmt.Errorf("theme: variant %q role %q: %q is not #RRGGBB", v, r, hex)
			}
		}
	}
	return t, nil
}

// MustParse parses tokens.json5 content, returning nil on malformed input.
// Kept for callers that treat malformed embedded tokens as fatal.
func MustParse(data []byte) Tokens {
	t, err := Parse(data)
	if err != nil {
		slog.Error("theme: malformed embedded tokens", "error", err)
		return nil
	}
	return t
}

// Hex returns the hex for variant/role, or "" when absent.
func (t Tokens) Hex(variant, role string) string {
	if roles, ok := t[variant]; ok {
		return roles[role]
	}
	return ""
}
