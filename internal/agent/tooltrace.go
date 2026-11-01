package agent

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/caimlas/meept/pkg/id"
)

// toolSpanMetadata carries one tool call's hop-tracing attributes
// (llm-hop-tracing leaf 01): the turn's trace, a per-call span, and the
// shape-only argument fingerprints. Produced by toolHopTrace; stamped onto
// the ToolExecutionEndData the loop emits after execution.
type toolSpanMetadata struct {
	TraceID    string
	SpanID     string
	ArgKeys    string
	ArgRawSize int
}

// toolHopTrace resolves one tool call's hop identity: the trace rides from
// the turn ("" on turns with no minted trace -- the pre-leaf posture), the
// span mints fresh per call, and the argument fingerprint is SHAPE-ONLY --
// sorted comma-joined key NAMES of the arguments JSON plus the raw
// arguments byte length. Unparseable arguments degrade to empty keys with
// the raw length; argument VALUES never enter any field.
func toolHopTrace(turnTraceID, rawArguments string) toolSpanMetadata {
	spanID := id.SpanID()
	keys := ""
	if rawArguments != "" {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(rawArguments), &parsed); err == nil {
			if len(parsed) > 0 {
				names := make([]string, 0, len(parsed))
				for k := range parsed {
					names = append(names, k)
				}
				sort.Strings(names)
				keys = strings.Join(names, ",")
			}
		}
	}
	return toolSpanMetadata{
		TraceID:    turnTraceID,
		SpanID:     spanID,
		ArgKeys:    keys,
		ArgRawSize: len(rawArguments),
	}
}

// stampToolHopTrace copies the hop attributes onto an end-event payload.
func stampToolHopTrace(data *ToolExecutionEndData, m toolSpanMetadata) {
	data.TraceID = m.TraceID
	data.SpanID = m.SpanID
	data.ArgumentsKeys = m.ArgKeys
	data.ArgumentsBytes = m.ArgRawSize
}

// blockReasonForToolResult reports the end event's block_reason: a
// before-tool-call deny carries its reason via the result's error prefix
// ("blocked by security policy: ..."); non-blocked results have none. The
// ExecutionResult shape has no dedicated blocked flag (the deny paths
// encode it in Error), so this mirrors the loop's own deny strings.
func blockReasonForToolResult(r *ExecutionResult) string {
	if r == nil {
		return ""
	}
	const prefix = "blocked by security policy: "
	if strings.HasPrefix(r.Error, prefix) {
		return strings.TrimPrefix(r.Error, prefix)
	}
	return ""
}
