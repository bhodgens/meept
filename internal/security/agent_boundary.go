// agent_boundary.go implements the inter-agent deny-pattern boundary
// (plan: docs/plans/20261031-agent-boundary-hardening/01-inter-agent-taint/
// 02-driver-wiring.md, contract C4-A; research decision D2 in
// docs/research/agent-boundary-taint-findings.md).
//
// The pattern set is deliberately narrow and PHRASE-LEVEL (OWASP prompt
// injection cheat sheet: single-word triggers produce documented false
// positives). It covers exactly the four D2 classes and nothing else:
// no jailbreak personas, no content-policy classes, no single suspicious
// words. It is pattern-level only -- no LLM classifier here.
//
// Callers scan only UNTRUSTED-SEGMENT content (raw peer output). Driver
// framing text is never passed here, so legitimate instructions the
// driver itself authored can never trip a pattern.
package security

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// InterAgentBoundaryPattern is one compiled deny pattern with its class
// (D2 taxonomy) and a human-readable reason fragment.
type InterAgentBoundaryPattern struct {
	Re     *regexp.Regexp
	Class  string
	Reason string
}

// InterAgentBoundaryMatch is one deny-pattern hit.
type InterAgentBoundaryMatch struct {
	Class   string
	Pattern string
	Reason  string
}

// D2 deny classes.
const (
	// D2 class 1: forged role/control markers.
	BoundaryClassRoleMarker = "forged_role_marker"
	// D2 class 2: instruction-override verbs.
	BoundaryClassOverrideVerb = "instruction_override"
	// D2 class 3: agent impersonation / control-verb forgery.
	BoundaryClassControlForgery = "control_verb_forgery"
	// D2 class 4: exfiltration directives.
	BoundaryClassExfiltration = "exfiltration_directive"
)

// interAgentBoundaryPatterns is the D2 inventory. Extend with care: every
// addition must stay phrase-level and cite its class.
var interAgentBoundaryPatterns = []InterAgentBoundaryPattern{
	// --- Class 1: forged role/control markers (extends the prompt_guard
	// inventory with meept-specific control envelopes). -------------------
	{regexp.MustCompile(`(?im)^\s*(system|assistant|developer|orchestrator)\s*:`), BoundaryClassRoleMarker, "forged role marker"},
	{regexp.MustCompile(`(?i)<\|im_start\|>|<\|im_end\|>|\[INST\]|\[/INST\]|<<SYS>>|<</SYS>>`), BoundaryClassRoleMarker, "forged chat-template special token"},
	{regexp.MustCompile(`(?i)"\s*type\s*"\s*:\s*"(task\.completed|turn\.terminal|agent\.quota_wait)"`), BoundaryClassRoleMarker, "forged bus/task event envelope"},
	{regexp.MustCompile(`(?im)^\s*##?\s*(SYSTEM|CONSTITUTION|OVERRIDES?|NEW (SYSTEM )?INSTRUCTIONS?)\b`), BoundaryClassRoleMarker, "forged system-prompt section header"},

	// --- Class 2: instruction-override verbs (phrase-level, mirrors the
	// prompt_guard.go override regexes). ----------------------------------
	{regexp.MustCompile(`(?i)ignore\s+(all\s+)?(the\s+)?(previous|prior|above|earlier)\s+instructions?`), BoundaryClassOverrideVerb, "instruction override: ignore previous instructions"},
	{regexp.MustCompile(`(?i)(disregard|forget|discard|override)\s+(all\s+)?(your\s+|the\s+|their\s+)?(previous\s+|prior\s+|above\s+)?instructions?`), BoundaryClassOverrideVerb, "instruction override: disregard instructions"},
	{regexp.MustCompile(`(?i)you\s+are\s+now\s+(a|an|the)\s+`), BoundaryClassOverrideVerb, "instruction override: role reassignment"},
	{regexp.MustCompile(`(?i)new\s+(system\s+)?instructions?\s*:`), BoundaryClassOverrideVerb, "instruction override: new instructions"},

	// --- Class 3: agent impersonation + control-verb forgery (the
	// LLM-to-LLM replication verbs from arXiv:2410.07283). ---------------
	{regexp.MustCompile(`(?i)\bi\s+am\s+(the\s+)?(agent|orchestrator|planner|daemon|system)\b`), BoundaryClassControlForgery, "agent impersonation claim"},
	{regexp.MustCompile(`(?i)\bi\s+am\s+agent\s+[a-z0-9_-]+`), BoundaryClassControlForgery, "agent impersonation claim"},
	{regexp.MustCompile(`(?i)\bas\s+(the\s+)?(orchestrator|daemon|system)\s*,`), BoundaryClassControlForgery, "impersonation authority claim"},
	{regexp.MustCompile(`(?i)\b(act|speak|respond)\s+(as|on\s+behalf\s+of)\s+(agent|the\s+(orchestrator|daemon|planner))\b`), BoundaryClassControlForgery, "impersonation directive"},
	{regexp.MustCompile(`(?i)\byield\s+the\s+workspace\b`), BoundaryClassControlForgery, "workspace_yield instruction forgery"},
	{regexp.MustCompile(`(?i)\bhand\s+off\s+(the\s+)?(turn|token|control)\s+to\b`), BoundaryClassControlForgery, "token handoff forgery"},
	{regexp.MustCompile(`(?i)\bspawn\s+(a\s+|an\s+|another\s+)?(agent|subagent|session|collaboration)\b`), BoundaryClassControlForgery, "spawn instruction forgery"},
	{regexp.MustCompile(`(?i)\brun\s+the\s+(gate|review|build)\b`), BoundaryClassControlForgery, "gate/review invocation forgery"},
	{regexp.MustCompile(`(?i)\b(approve|accept)\s+the\s+(plan|task|turn|changes?|yield)\b`), BoundaryClassControlForgery, "approval forgery"},
	{regexp.MustCompile(`(?i)\brequest(_|\s)token\b`), BoundaryClassControlForgery, "control-verb forgery: request_token"},
	{regexp.MustCompile(`(?i)"\s*action\s*"\s*:\s*"(approve|request_changes|request_token)"`), BoundaryClassControlForgery, "spoofed control-JSON block"},
	{regexp.MustCompile(`(?i)\bsend\s+(this\s+)?(message|output|result|content)\s+to\s+(the\s+)?agent\b`), BoundaryClassControlForgery, "relay directive to another agent"},
	{regexp.MustCompile(`(?i)\bsend\s+it\s+to\s+(the\s+)?agent\b`), BoundaryClassControlForgery, "relay directive to another agent"},

	// --- Class 4: exfiltration directives (credential-bearing URL
	// patterns from taint_hooks.go plus send-to-external phrasing). -------
	{regexp.MustCompile(`(?i)(api_key|apikey|access_token|auth_token|client_secret)=`), BoundaryClassExfiltration, "credential-bearing URL parameter"},
	{regexp.MustCompile(`(?i)\b(bearer|authorization)\s*:\s*[a-z0-9._-]{8,}`), BoundaryClassExfiltration, "inline credential header"},
	{regexp.MustCompile(`(?i)\b(send|post|upload|forward|transmit|exfiltrate|leak)\s+(the\s+)?(api[_ ]?key|secret|token|password|credential|env|\.env)\b`), BoundaryClassExfiltration, "secret relay directive"},
	{regexp.MustCompile(`(?i)\b(webhook\.site|requestbin|pipedream\.net|ngrok\.(io|app)|pastebin\.com|\.onion\b)`), BoundaryClassExfiltration, "known exfiltration endpoint"},
}

// InterAgentBoundary scans UNTRUSTED handoff content for the D2 deny
// patterns. Construction is infallible; nil logger falls back to the
// default. The zero value is NOT usable -- build through
// NewInterAgentBoundary.
type InterAgentBoundary struct {
	logger   *slog.Logger
	patterns []InterAgentBoundaryPattern
}

// NewInterAgentBoundary creates the boundary with the default D2 pattern
// inventory (contract C4-A).
func NewInterAgentBoundary(logger *slog.Logger) *InterAgentBoundary {
	if logger == nil {
		logger = slog.Default()
	}
	// Copy the package-level slice so a future hot-reload of patterns
	// cannot mutate live boundaries.
	patterns := make([]InterAgentBoundaryPattern, len(interAgentBoundaryPatterns))
	copy(patterns, interAgentBoundaryPatterns)
	return &InterAgentBoundary{
		logger:   logger.With("component", "inter-agent-boundary"),
		patterns: patterns,
	}
}

// InspectHandoffContent scans content (an UNTRUSTED handoff segment) and
// returns every deny-pattern hit. Empty content returns no matches. This
// is the decision-free primitive; callers own the mode policy
// (off/warn/block).
func (b *InterAgentBoundary) InspectHandoffContent(content string) []InterAgentBoundaryMatch {
	if b == nil || content == "" {
		return nil
	}
	var matches []InterAgentBoundaryMatch
	for i := range b.patterns {
		p := &b.patterns[i]
		if loc := p.Re.FindStringIndex(content); loc != nil {
			matches = append(matches, InterAgentBoundaryMatch{
				Class:   p.Class,
				Pattern: clipPatternSample(content[loc[0]:loc[1]]),
				Reason:  p.Reason,
			})
		}
	}
	return matches
}

// InspectHandoff checks a composed cross-agent handoff (contract C4-A
// signature): recipient identifies the receiving agent, content is the
// payload. Returns blocked=true with a reason when a deny pattern hits.
// Under the warn-first rollout this is consumed by the block-mode caller;
// monitor-mode callers log the matches and let the handoff pass.
func (b *InterAgentBoundary) InspectHandoff(recipient, content string) (blocked bool, reason string) {
	if b == nil {
		return false, ""
	}
	matches := b.InspectHandoffContent(content)
	if len(matches) == 0 {
		return false, ""
	}
	m := matches[0]
	b.logger.Warn("inter-agent handoff deny-pattern hit",
		"recipient", recipient,
		"class", m.Class,
		"reason", m.Reason,
		"sample", m.Pattern,
	)
	return true, fmt.Sprintf("handoff to %s blocked: %s (%s)", recipient, m.Reason, m.Class)
}

// clipPatternSample keeps matched text bounded and single-line for logs.
func clipPatternSample(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}
