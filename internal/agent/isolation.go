package agent

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/llm"
)

// warnUnknownIsolationOnce gates the fail-closed warning so a hot spawn loop
// cannot spam the log; the contract requires warning "once".
var warnUnknownIsolationOnce sync.Once

// ContextIsolation controls what context a child agent (handoff target,
// subagent, or collaboration participant) receives when it is spawned.
//
// The default is ArtifactOnly: children get a structured brief plus artifact
// references, never the parent's raw message transcript. SharedTranscript is
// an explicit opt-in for modes that genuinely need the full conversation
// (none today); BusMessage defers context delivery to the message bus.
type ContextIsolation string

const (
	// IsolationArtifactOnly is the default isolation level. The child
	// receives Brief + Artifacts + MemoryIDs; Transcript is empty.
	IsolationArtifactOnly ContextIsolation = "artifact_only"

	// IsolationSharedTranscript explicitly opts the child into receiving a
	// copy of the parent's message transcript. Use sparingly: the transcript
	// may contain full tool-result dumps and chain-of-thought.
	IsolationSharedTranscript ContextIsolation = "shared_transcript"

	// IsolationBusMessage leaves the Transcript empty; the parent delivers
	// context to the child over the bus after spawn.
	IsolationBusMessage ContextIsolation = "bus_message"
)

// ArtifactRef points at a durable artifact produced by the parent (file,
// report, rendered handoff markdown, ...) that the child may consult instead
// of receiving the parent's raw conversation.
type ArtifactRef struct {
	Path   string
	SHA256 string
}

// SpawnContext is the complete context handed to a child agent at spawn time.
// Transcript is populated ONLY when Isolation == IsolationSharedTranscript;
// for every other isolation level it must be empty (fail closed).
type SpawnContext struct {
	Isolation  ContextIsolation
	Brief      string
	Artifacts  []ArtifactRef
	MemoryIDs  []string
	Transcript []llm.ChatMessage // populated ONLY when Isolation == SharedTranscript
}

// BuildSpawnContext constructs a SpawnContext for a child spawn under the
// requested isolation level. parent is the parent conversation's messages;
// it is copied into the child ONLY for IsolationSharedTranscript.
//
// Unknown isolation values fail closed to IsolationArtifactOnly with a
// one-time warning rather than falling back to a transcript copy.
func BuildSpawnContext(iso ContextIsolation, brief string, artifacts []ArtifactRef, memoryIDs []string, parent []llm.ChatMessage) SpawnContext {
	switch iso {
	case IsolationSharedTranscript:
		transcript := make([]llm.ChatMessage, len(parent))
		copy(transcript, parent)
		return SpawnContext{
			Isolation:  IsolationSharedTranscript,
			Brief:      brief,
			Artifacts:  artifacts,
			MemoryIDs:  memoryIDs,
			Transcript: transcript,
		}
	case IsolationBusMessage:
		// Transcript stays empty: the parent sends context over the bus
		// after spawn, so copying it here would defeat the isolation point.
		return SpawnContext{
			Isolation: IsolationBusMessage,
			Brief:     brief,
			Artifacts: artifacts,
			MemoryIDs: memoryIDs,
		}
	case IsolationArtifactOnly:
		return SpawnContext{
			Isolation: IsolationArtifactOnly,
			Brief:     brief,
			Artifacts: artifacts,
			MemoryIDs: memoryIDs,
		}
	default:
		warnUnknownIsolationOnce.Do(func() {
			slog.Warn("context isolation: unknown isolation value; failing closed to artifact_only",
				"requested", string(iso),
			)
		})
		return BuildSpawnContext(IsolationArtifactOnly, brief, artifacts, memoryIDs, parent)
	}
}

// RenderSpawnContext renders a SpawnContext as the brief block prepended to a
// child's input message. Transcript content is never rendered; the brief is a
// structured summary, not a conversation dump.
func RenderSpawnContext(sc SpawnContext) string {
	var sb strings.Builder
	if sc.Brief != "" {
		sb.WriteString(sc.Brief)
	}
	if len(sc.Artifacts) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("## Prior Artifacts\n")
		for _, a := range sc.Artifacts {
			if a.SHA256 != "" {
				fmt.Fprintf(&sb, "- %s (sha256: %s)\n", a.Path, a.SHA256)
				continue
			}
			sb.WriteString("- " + a.Path + "\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
