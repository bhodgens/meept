package agent

// 04-clutter/01 server reply hygiene (gate T1, F-ORCH-15): a completed
// chat turn must NEVER ship an empty reply. When the loop terminated on a
// blank stop after a tool-only turn (the frozen p1 shape: work ran,
// reply=""), the turn's tool ledger is the only ground truth: synthesize an
// honest, user-shaped summary of the RECORDED effects only (paths written,
// commands run). Synthesis never fabricates an effect absent from the
// ledger; with no ledger evidence the fallback says exactly that. The C1
// reply_source key ("synthesized") marks ledger-derived replies so
// consumers can tell a real answer from a reconstruction.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// replySourceSynthesized is the C1 reply_source value for a reply
// reconstructed from the turn's tool ledger (never loop prose).
const replySourceSynthesized = "synthesized"

// replySourceTaskRelay is the C1 reply_source value for a reply relayed
// from a task-completion/failure event rather than produced by a live loop
// turn on this request.
const replySourceTaskRelay = "task_relay"

// turnToolEffect is one recorded tool call with its rendered argument
// summary and (when the conversation carries it) the tool result text.
type turnToolEffect struct {
	id     string
	name   string
	detail string
	result string
}

// argDetailKeys are the argument keys that name the EFFECT of a call, in
// display-preference order: the artifact path for file tools, the command
// for executors, then generic targets.
var argDetailKeys = []string{"path", "file", "filename", "file_path", "command", "cmd", "url", "query", "pattern"}

// synthesizeCompletedReply (gate T1): produce a non-empty, ledger-bound
// reply for a completed turn whose loop returned blank text. Returns the
// reply and its C1 reply_source ("synthesized" when the ledger carried
// effects; "" for the honest no-evidence fallback). The reply NEVER claims
// an effect the ledger does not record.
func (h *ChatHandler) synthesizeCompletedReply(conversationID string) (string, string) {
	effects := h.turnToolLedger(conversationID)
	if len(effects) == 0 {
		return "Your turn finished without a final summary and without any " +
			"recorded actions, so nothing was changed on this turn. If you " +
			"expected a result, rephrase the request or send it again.", ""
	}
	return renderLedgerReply(effects), replySourceSynthesized
}

// turnToolLedger collects the current turn's tool effects from the loop
// conversation: the session-scoped dispatch loops first (the route_to_agent
// lane runs per-session loops keyed cacheKey(sessionID, agentID), so every
// loop owned by this conversation is checked), then the singleton loop
// (direct mode). Read-only and best-effort: a missing conversation yields nil.
func (h *ChatHandler) turnToolLedger(conversationID string) []turnToolEffect {
	if conversationID == "" {
		return nil
	}
	if h.dispatcher != nil && h.dispatcher.loopManager != nil {
		prefix := conversationID + "\x00"
		for key, loop := range h.dispatcher.loopManager.List() {
			if loop == nil || (key != conversationID && !strings.HasPrefix(key, prefix)) {
				continue
			}
			if effects := collectTurnEffects(loop.GetConversation(conversationID)); len(effects) > 0 {
				return effects
			}
		}
	}
	if h.loop != nil {
		return collectTurnEffects(h.loop.GetConversation(conversationID))
	}
	return nil
}

// collectTurnEffects walks a conversation tail (messages after the LAST
// user message, the current-turn boundary) pairing assistant tool calls
// with their tool results. A missing conversation yields nil.
func collectTurnEffects(conv *Conversation) []turnToolEffect {
	if conv == nil {
		return nil
	}
	var calls []turnToolEffect
	results := map[string]string{}
	for _, m := range conv.GetMessages() {
		switch m.Role {
		case llm.RoleUser:
			// New turn boundary: drop everything collected so far.
			calls = nil
			results = map[string]string{}
		case llm.RoleAssistant:
			for _, tc := range m.ToolCalls {
				calls = append(calls, turnToolEffect{
					id:     tc.ID,
					name:   tc.Function.Name,
					detail: summarizeToolArgs(tc.Function.Arguments),
				})
			}
		case llm.RoleTool:
			if m.ToolCallID != "" {
				results[m.ToolCallID] = strings.TrimSpace(m.Content)
			}
		}
	}
	for i := range calls {
		if r, ok := results[calls[i].id]; ok {
			calls[i].result = r
		}
	}
	return calls
}

// summarizeToolArgs extracts the effect-naming argument (path, command,
// url, ...) from a tool call's JSON arguments. Malformed or keyless JSON
// degrades to the longest string value; unparseable input renders nothing
// (the call is still listed by name).
func summarizeToolArgs(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return ""
	}
	for _, key := range argDetailKeys {
		if s, ok := args[key].(string); ok && strings.TrimSpace(s) != "" {
			return oneLine(strings.TrimSpace(s))
		}
	}
	best := ""
	for _, v := range args {
		if s, ok := v.(string); ok && len(s) > len(best) {
			best = strings.TrimSpace(s)
		}
	}
	return truncateString(oneLine(best), 160)
}

// renderLedgerReply builds the user-shaped summary of recorded effects,
// line-capped so a huge tool-only turn cannot ship an unbounded reply.
func renderLedgerReply(effects []turnToolEffect) string {
	const maxLines = 12
	var sb strings.Builder
	sb.WriteString("Completed this turn without a final summary. Recorded actions:\n")
	for i, e := range effects {
		if i >= maxLines {
			fmt.Fprintf(&sb, "- ... and %d more\n", len(effects)-i)
			break
		}
		line := e.name
		if e.detail != "" {
			line += ": " + truncateString(e.detail, 240)
		}
		if e.result != "" {
			line += " (" + truncateString(oneLine(e.result), 160) + ")"
		}
		sb.WriteString("- " + line + "\n")
	}
	return strings.TrimSpace(sb.String())
}

// oneLine collapses a multi-line blob to its first non-empty line.
func oneLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// truncationDisclosure (gate T4c, F-NEW-8): consumes the one-shot
// firewall truncation notices for this conversation's loops and returns
// the user-visible disclosure (never silent). The AgentLoop holds the
// context firewall reference in-package, so the handler reads it directly;
// the notice self-clears on first read so one truncation discloses once.
func (h *ChatHandler) truncationDisclosure(conversationID string) string {
	if conversationID == "" {
		return ""
	}
	notice := ""
	collect := func(loop *AgentLoop) {
		if loop == nil || loop.contextFirewall == nil {
			return
		}
		if n := loop.contextFirewall.TruncationNotice(); n != "" {
			notice = n
		}
	}
	if h.dispatcher != nil && h.dispatcher.loopManager != nil {
		prefix := conversationID + "\x00"
		for key, loop := range h.dispatcher.loopManager.List() {
			if key == conversationID || strings.HasPrefix(key, prefix) {
				collect(loop)
			}
		}
	}
	if h.loop != nil {
		collect(h.loop)
	}
	return notice
}
