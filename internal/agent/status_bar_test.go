package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// golden matrix for StatusBar (contract C8): compact lowercase block, fixed
// field order, fail-closed to "unknown" / "n/a" everywhere.
func TestStatusBarGolden(t *testing.T) {
	tests := []struct {
		name string
		in   TurnStatus
		want string
	}{
		{
			name: "zero value: all unknowns, turn=0 tools=0, gate n/a",
			in:   TurnStatus{},
			want: "[status] turn=0 tools=0 isolation=unknown speak=unknown gate=n/a",
		},
		{
			name: "full passing turn",
			in: TurnStatus{
				TurnIndex:       3,
				ToolsThisTurn:   5,
				Isolation:       IsolationArtifactOnly,
				Speak:           SpeakNotify,
				SessionAttached: false,
				IsolatedChild:   false,
				GateState:       "passed",
			},
			want: "[status] turn=3 tools=5 isolation=artifact_only speak=notify gate=passed",
		},
		{
			name: "empty isolation renders unknown",
			in: TurnStatus{
				TurnIndex:     1,
				ToolsThisTurn: 2,
				Isolation:     "",
				Speak:         SpeakNotify,
				GateState:     "skipped",
			},
			want: "[status] turn=1 tools=2 isolation=unknown speak=notify gate=skipped",
		},
		{
			name: "unrecognized isolation value renders unknown, never echoed",
			in: TurnStatus{
				TurnIndex:       7,
				ToolsThisTurn:   0,
				Isolation:       ContextIsolation("leaked_parent_transcript"),
				Speak:           SpeakSession,
				SessionAttached: true,
				GateState:       "failed",
			},
			want: "[status] turn=7 tools=0 isolation=unknown speak=session gate=failed",
		},
		{
			name: "session attached kind",
			in: TurnStatus{
				TurnIndex:       4,
				ToolsThisTurn:   9,
				Isolation:       IsolationBusMessage,
				Speak:           SpeakSession,
				SessionAttached: true,
				GateState:       "",
			},
			want: "[status] turn=4 tools=9 isolation=bus_message speak=session gate=n/a",
		},
		{
			name: "isolated child trumps attached: speak=parent (C4 fail-closed)",
			in: TurnStatus{
				TurnIndex:       2,
				ToolsThisTurn:   1,
				Isolation:       IsolationArtifactOnly,
				Speak:           SpeakSession,
				SessionAttached: true,
				IsolatedChild:   true,
				GateState:       "passed",
			},
			want: "[status] turn=2 tools=1 isolation=artifact_only speak=parent gate=passed",
		},
		{
			name: "parent kind without isolated bit",
			in: TurnStatus{
				TurnIndex:     11,
				ToolsThisTurn: 3,
				Isolation:     IsolationSharedTranscript,
				Speak:         SpeakParent,
				GateState:     "skipped",
			},
			want: "[status] turn=11 tools=3 isolation=shared_transcript speak=parent gate=skipped",
		},
		{
			name: "gate did not run this turn: empty renders n/a",
			in: TurnStatus{
				TurnIndex:     5,
				ToolsThisTurn: 4,
				Isolation:     IsolationArtifactOnly,
				Speak:         SpeakNotify,
				GateState:     "",
			},
			want: "[status] turn=5 tools=4 isolation=artifact_only speak=notify gate=n/a",
		},
		{
			name: "invalid gate state fails closed to unknown, never invented success",
			in: TurnStatus{
				TurnIndex:     6,
				ToolsThisTurn: 8,
				Isolation:     IsolationArtifactOnly,
				Speak:         SpeakNotify,
				GateState:     "pass",
			},
			want: "[status] turn=6 tools=8 isolation=artifact_only speak=notify gate=unknown",
		},
		{
			name: "wrong-case gate state fails closed",
			in: TurnStatus{
				TurnIndex:     6,
				ToolsThisTurn: 8,
				Isolation:     IsolationArtifactOnly,
				Speak:         SpeakNotify,
				GateState:     "PASSED",
			},
			want: "[status] turn=6 tools=8 isolation=artifact_only speak=notify gate=unknown",
		},
		{
			name: "out-of-range speak kind fails closed to unknown",
			in: TurnStatus{
				TurnIndex:       8,
				ToolsThisTurn:   2,
				Isolation:       IsolationArtifactOnly,
				Speak:           SpeakKind(42),
				SessionAttached: true,
				GateState:       "passed",
			},
			want: "[status] turn=8 tools=2 isolation=artifact_only speak=unknown gate=passed",
		},
		{
			name: "negative speak kind fails closed to unknown",
			in: TurnStatus{
				TurnIndex:     9,
				ToolsThisTurn: 0,
				Isolation:     IsolationArtifactOnly,
				Speak:         SpeakKind(-1),
				GateState:     "passed",
			},
			want: "[status] turn=9 tools=0 isolation=artifact_only speak=unknown gate=passed",
		},
		{
			name: "isolated child with zero speak still renders parent",
			in: TurnStatus{
				TurnIndex:     1,
				ToolsThisTurn: 1,
				Isolation:     IsolationArtifactOnly,
				IsolatedChild: true,
			},
			want: "[status] turn=1 tools=1 isolation=artifact_only speak=parent gate=n/a",
		},
		{
			name: "large counts render verbatim",
			in: TurnStatus{
				TurnIndex:     12345,
				ToolsThisTurn: 678,
				Isolation:     IsolationArtifactOnly,
				Speak:         SpeakNotify,
				GateState:     "failed",
			},
			want: "[status] turn=12345 tools=678 isolation=artifact_only speak=notify gate=failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StatusBar(tt.in)
			if got != tt.want {
				t.Fatalf("StatusBar() =\n  %q\nwant:\n  %q", got, tt.want)
			}
			// Determinism: repeated calls are byte-identical.
			if again := StatusBar(tt.in); again != got {
				t.Fatalf("StatusBar not deterministic: %q vs %q", got, again)
			}
			// The bar is one compact line, no model prose, no clock.
			if strings.ContainsAny(got, "\n\r") {
				t.Fatalf("StatusBar must be single-line, got %q", got)
			}
		})
	}
}

// C8: the bar never copies message/model text into itself and always starts
// with the [status] marker.
func TestStatusBarNoProseInjection(t *testing.T) {
	got := StatusBar(TurnStatus{
		TurnIndex:     1,
		ToolsThisTurn: 1,
		Isolation:     IsolationArtifactOnly,
		Speak:         SpeakNotify,
	})
	for _, banned := range []string{"```", "user", "assistant", "model=", "timestamp"} {
		if strings.Contains(got, banned) {
			t.Fatalf("StatusBar contains banned token %q: %q", banned, got)
		}
	}
	if !strings.HasPrefix(got, "[status] turn=") {
		t.Fatalf("StatusBar must start with the [status] marker, got %q", got)
	}
}

// PromptSection integration: the status section is UNSTABLE by contract, so
// it must land after the stable prefix and must not perturb the hash.
func TestStatusSectionIsUnstable(t *testing.T) {
	sections := []PromptSection{
		{Name: "constitution", Stable: true, Body: "# Constitution\nbe helpful"},
		{Name: "cache-boundary", Stable: true, Body: "STABLE-BOUNDARY"},
		{Name: "status", Stable: false, Body: StatusBar(TurnStatus{TurnIndex: 3, ToolsThisTurn: 5, Speak: SpeakNotify})},
	}

	prompt, hash := AssembleOrdered(sections)
	if !strings.Contains(prompt, "[status] turn=3 tools=5") {
		t.Fatalf("assembled prompt missing status block: %q", prompt)
	}
	if idx := strings.Index(prompt, "[status]"); idx < strings.Index(prompt, "STABLE-BOUNDARY") {
		t.Fatalf("status section must follow the stable prefix, prompt: %q", prompt)
	}

	// Changing ONLY the status body keeps the stable prefix hash identical
	// (leaf 13 Task 5: two consecutive builds differing only in TurnStatus
	// produce the SAME stablePrefixHash).
	sections[2].Body = StatusBar(TurnStatus{TurnIndex: 4, ToolsThisTurn: 5, Speak: SpeakNotify, GateState: "failed"})
	prompt2, hash2 := AssembleOrdered(sections)
	if hash2 != hash {
		t.Fatalf("stable prefix hash changed when only status changed:\n  before: %s\n  after:  %s", hash, hash2)
	}
	if prompt2 == prompt {
		t.Fatal("expected the assembled prompt body to change when status changed")
	}
}

// End-to-end through the loop's own assembler (assembleSystemPrompt), which
// injects the status section itself: two consecutive builds differing only
// in TurnStatus (turn counter bumped between builds) must leave
// LastStablePrefixHash untouched.
func TestAssembleSystemPromptStablePrefixIgnoresStatus(t *testing.T) {
	loop := NewAgentLoop("status-bar-hash", "/tmp")

	newBuilder := func() *PromptBuilder {
		b := NewPromptBuilder()
		b.AddSectionWithStability("Platform Capabilities", "capabilities body", true)
		return b
	}

	first := loop.assembleSystemPrompt(newBuilder())
	hashAfterFirst := loop.LastStablePrefixHash()
	if hashAfterFirst == "" {
		t.Fatal("expected a recorded stable prefix hash after first build")
	}
	if !strings.Contains(first, "[status] turn=0") {
		t.Fatalf("first build missing status block: %q", first)
	}

	// Bump ONLY the loop's TurnStatus input (turn index): no stable-section
	// state changes between builds.
	loop.mu.Lock()
	loop.turnCounter = 4
	loop.mu.Unlock()

	second := loop.assembleSystemPrompt(newBuilder())
	hashAfterSecond := loop.LastStablePrefixHash()

	if hashAfterSecond != hashAfterFirst {
		t.Fatalf("stable prefix hash drifted when only TurnStatus changed:\n  first:  %s\n  second: %s", hashAfterFirst, hashAfterSecond)
	}
	if !strings.Contains(second, "[status] turn=4") {
		t.Fatalf("second build missing updated status block: %q", second)
	}
	if first == second {
		t.Fatal("expected the assembled prompt to change when the turn status changed")
	}
}

// turnStatus snapshot: populated from loop-visible state, gate n/a at
// assembly time, and speak/isolation consistent with the attachment bits.
func TestLoopTurnStatusSnapshot(t *testing.T) {
	// Session-attached default loop.
	loop := NewAgentLoop("status-bar-snapshot", "/tmp")
	st := loop.turnStatus()
	if st.TurnIndex != 0 || st.ToolsThisTurn != 0 {
		t.Fatalf("expected zero turn/tools for a fresh loop, got %+v", st)
	}
	if st.GateState != "" {
		t.Fatalf("gate state must be empty at assembly time, got %q", st.GateState)
	}
	if st.SessionAttached != true || st.IsolatedChild != false {
		t.Fatalf("unexpected attachment bits: %+v", st)
	}
	if got, want := st.Speak, ClassifyRun(true, false); got != want {
		t.Fatalf("Speak = %v, want %v", got, want)
	}
	if st.Isolation != "" {
		t.Fatalf("non-isolated loop must report empty isolation, got %q", st.Isolation)
	}

	// Isolated child loop: artifact_only + parent speak (C4).
	child := NewAgentLoop("status-bar-child", "/tmp", WithIsolatedChild(true))
	cs := child.turnStatus()
	if cs.Isolation != IsolationArtifactOnly {
		t.Fatalf("isolated child must report artifact_only, got %q", cs.Isolation)
	}
	if got := StatusBar(cs); !strings.Contains(got, "speak=parent") || !strings.Contains(got, "isolation=artifact_only") {
		t.Fatalf("isolated child bar wrong: %q", got)
	}
}

// Tool-index stability (leaf 13 Task 3/4): in indexed mode the COMPACT tool
// index (name + stub description, no full JSON schemas) must be
// byte-invariant within a session and ride a Stable prompt section.
func TestIndexedToolListStableAndHashInvariant(t *testing.T) {
	reg := tools.NewRegistry(nil)
	reg.Register(statusStubTool{name: "alpha", desc: "Alpha tool."})
	reg.Register(statusStubTool{name: "beta", desc: "Beta tool."})
	reg.SetSchemaMode(tools.SchemaModeIndexed, []string{"tool_view"})

	list1 := compactToolIndex(reg.ToLLMDefinitions())
	if !strings.Contains(list1, "alpha") || !strings.Contains(list1, "tool_view{beta}") {
		t.Fatalf("compact tool index missing stub markers: %q", list1)
	}
	// No full JSON schemas in the compact index.
	if strings.Contains(list1, "\"properties\"") || strings.Contains(list1, "\"type\": \"object\"") {
		t.Fatalf("compact tool index must not carry full schemas: %q", list1)
	}

	sum := sha256.Sum256([]byte(list1))
	want := hex.EncodeToString(sum[:])

	list2 := compactToolIndex(reg.ToLLMDefinitions())
	if list2 != list1 {
		t.Fatalf("compact tool index not byte-stable across calls:\n  first:  %q\n  second: %q", list1, list2)
	}
	sum2 := sha256.Sum256([]byte(list2))
	if hex.EncodeToString(sum2[:]) != want {
		t.Fatal("tool index hash changed without any state change")
	}

	// As a Stable section it must not perturb the stable prefix hash: a
	// status-only change beside a byte-identical tool index keeps the hash.
	toolSection := PromptSection{Name: "tool-index", Stable: true, Body: list1}
	statusA := PromptSection{Name: "status", Stable: false, Body: StatusBar(TurnStatus{TurnIndex: 1, ToolsThisTurn: 3})}
	statusB := PromptSection{Name: "status", Stable: false, Body: StatusBar(TurnStatus{TurnIndex: 2, ToolsThisTurn: 3})}
	_, hashA := AssembleOrdered([]PromptSection{{Name: "c", Stable: true, Body: "const"}, toolSection, statusA})
	_, hashB := AssembleOrdered([]PromptSection{{Name: "c", Stable: true, Body: "const"}, toolSection, statusB})
	if hashA != hashB {
		t.Fatalf("stable prefix hash drifted with a byte-identical tool index:\n  a: %s\n  b: %s", hashA, hashB)
	}
}

// statusStubTool is a minimal tools.Tool for registry-level tests.
type statusStubTool struct {
	name string
	desc string
}

func (s statusStubTool) Name() string        { return s.name }
func (s statusStubTool) Description() string { return s.desc }
func (s statusStubTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: "object", Properties: map[string]llm.ParameterProperty{}}
}
func (s statusStubTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return "ok", nil
}
func (s statusStubTool) IsReadOnly(map[string]any) bool        { return true }
func (s statusStubTool) IsConcurrencySafe(map[string]any) bool { return true }
