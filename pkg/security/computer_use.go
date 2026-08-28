package security

import "strings"

// CuaDriverToolPrefix is the registered-name prefix for tools exposed by the
// cua-driver MCP server. MCP tools register as "<server>.<tool>" (see
// internal/tools/mcp), so cua-driver's capture tool is "cua-driver.capture".
const CuaDriverToolPrefix = "cua-driver."

// computerUseObservationActions are read-only desktop observation actions.
// They reveal screen contents but never inject input, so they classify LOW.
var computerUseObservationActions = map[string]bool{
	"capture":          true,
	"screenshot":       true,
	"list_apps":        true,
	"list_windows":     true,
	"get_window_state": true,
}

// ComputerUseRule classifies a registered tool name that belongs to the
// cua-driver MCP server. It returns the risk level and whether the name is
// a cua-driver tool at all.
//
// Classification is prefix-matched on the final registered name
// ("cua-driver.<action>"):
//   - known observation actions (capture/screenshot/list*/get_*) -> LOW
//   - everything else, INCLUDING unknown future actions           -> HIGH
//
// The unknown case deliberately fails closed: input injection is the default
// consequence of misclassifying an unseen action name, and HIGH risk is
// confirmation-gated under require_confirmation_high.
func ComputerUseRule(action string) (RiskLevel, bool) {
	rest, ok := strings.CutPrefix(action, CuaDriverToolPrefix)
	if !ok {
		return RiskSafe, false
	}
	if rest == "" {
		// Degenerate registered name under our namespace: still ours, still
		// unclassifiable -> fail closed at HIGH rather than fall through to
		// the generic unknown-action path.
		return RiskHigh, true
	}

	if strings.HasPrefix(rest, "list_") || strings.HasPrefix(rest, "get_") ||
		computerUseObservationActions[rest] {
		return RiskLow, true
	}

	return RiskHigh, true
}

// BrowserToolPrefix is the registered-name prefix for the built-in browser
// automation tool family (internal/browser + internal/tools/builtin/browser).
const BrowserToolPrefix = "browser_"

// browserObservationActions are read-only browser actions: they reveal page
// content but never inject input or navigate.
var browserObservationActions = map[string]bool{
	"read_text":  true,
	"screenshot": true,
	// Closing the session releases resources without injecting anything.
	"close": true,
}

// browserInputActions mutate browser state: navigation changes the loaded
// origin; click/type can submit forms and trigger side effects.
var browserInputActions = map[string]bool{
	"navigate": true,
	"type":     true,
}

// BrowserToolRule classifies a registered name from the built-in browser
// tool family, mirroring ComputerUseRule's fail-closed shape:
//   - observation actions (read_text/screenshot/close) -> LOW
//   - input actions (click/navigate/type)              -> HIGH
//   - unknown browser_* names                          -> HIGH (fail closed)
func BrowserToolRule(action string) (RiskLevel, bool) {
	rest, ok := strings.CutPrefix(action, BrowserToolPrefix)
	if !ok {
		return RiskSafe, false
	}
	if rest == "" {
		// Degenerate name under our namespace: fail closed.
		return RiskHigh, true
	}
	if strings.HasPrefix(rest, "click") || browserInputActions[rest] {
		return RiskHigh, true
	}
	if browserObservationActions[rest] {
		return RiskLow, true
	}
	return RiskHigh, true
}
