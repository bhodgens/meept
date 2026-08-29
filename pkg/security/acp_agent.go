package security

// ACPAgentToolName is the registered name of the ACP meta-tool.
// The verb lives in args (details["verb"]), not in the tool name.
const ACPAgentToolName = "acp_agent"

// ACPAgentRule classifies an acp_agent call by its verb argument.
//
// Classification (frozen contract):
//   - launch|send -> HIGH (spawn a process / inject content)
//   - read|stop   -> LOW
//   - unknown or missing verb -> HIGH (fail-closed)
//
// Other tool names are not classified (ok=false).
func ACPAgentRule(action string, details map[string]string) (RiskLevel, bool) {
	if action != ACPAgentToolName {
		return RiskSafe, false
	}
	verb, ok := details["verb"]
	if !ok {
		return RiskHigh, true
	}
	switch verb {
	case "launch", "send":
		return RiskHigh, true
	case "read", "stop":
		return RiskLow, true
	default:
		return RiskHigh, true
	}
}
