package agent

import (
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// isolationTestCanary is the marker string planted in the parent transcript;
// the canary tests assert it never reaches the child under ArtifactOnly.
const isolationTestCanary = "PARENT_TOOL_DUMP_CANARY"

// isolationParentMessages builds a parent transcript containing assistant
// chain-of-thought, a tool-result dump carrying the canary, and a user turn.
func isolationParentMessages() []llm.ChatMessage {
	return []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "investigate the failing build"},
		{Role: llm.RoleAssistant, Content: "Let me think about what could cause this..."},
		{Role: llm.RoleTool, Name: "shell", Content: isolationTestCanary + " giant tool output dump"},
		{Role: llm.RoleAssistant, Content: "Based on the tool output I conclude..."},
	}
}

// TestBuildSpawnContext_Matrix is the 3x isolation matrix: for each isolation
// level, Transcript population, Brief, Artifacts, and MemoryIDs preservation.
func TestBuildSpawnContext_Matrix(t *testing.T) {
	parent := isolationParentMessages()
	artifacts := []ArtifactRef{
		{Path: "/tmp/artifacts/report.md", SHA256: "abc123"},
		{Path: "/tmp/artifacts/diff.patch"},
	}
	memories := []string{"mem-1", "mem-2"}
	brief := "Fix the failing build; see prior artifacts."

	tests := []struct {
		name            string
		iso             ContextIsolation
		wantTranscript  bool
		wantTransLen    int
		wantIsolation   ContextIsolation
		wantBrief       string
		wantArtifacts   int
		wantMemoryIDs   int
		unchangedParent bool // parent slice must not be mutated
	}{
		{
			name:            "artifact_only keeps brief artifacts memories empties transcript",
			iso:             IsolationArtifactOnly,
			wantTranscript:  false,
			wantIsolation:   IsolationArtifactOnly,
			wantBrief:       brief,
			wantArtifacts:   2,
			wantMemoryIDs:   2,
			unchangedParent: true,
		},
		{
			name:            "shared_transcript copies parent messages plus brief artifacts",
			iso:             IsolationSharedTranscript,
			wantTranscript:  true,
			wantTransLen:    len(parent),
			wantIsolation:   IsolationSharedTranscript,
			wantBrief:       brief,
			wantArtifacts:   2,
			wantMemoryIDs:   2,
			unchangedParent: true,
		},
		{
			name:            "bus_message empties transcript parent sends on bus later",
			iso:             IsolationBusMessage,
			wantTranscript:  false,
			wantIsolation:   IsolationBusMessage,
			wantBrief:       brief,
			wantArtifacts:   2,
			wantMemoryIDs:   2,
			unchangedParent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parentBefore := len(parent)
			sc := BuildSpawnContext(tt.iso, brief, artifacts, memories, parent)

			if sc.Isolation != tt.wantIsolation {
				t.Errorf("Isolation = %q, want %q", sc.Isolation, tt.wantIsolation)
			}
			if got := len(sc.Transcript) > 0; got != tt.wantTranscript {
				t.Errorf("Transcript populated = %v (%d msgs), want populated = %v", got, len(sc.Transcript), tt.wantTranscript)
			}
			if tt.wantTranscript && len(sc.Transcript) != tt.wantTransLen {
				t.Errorf("Transcript length = %d, want %d", len(sc.Transcript), tt.wantTransLen)
			}
			if sc.Brief != tt.wantBrief {
				t.Errorf("Brief = %q, want %q", sc.Brief, tt.wantBrief)
			}
			if len(sc.Artifacts) != tt.wantArtifacts {
				t.Errorf("Artifacts = %d, want %d", len(sc.Artifacts), tt.wantArtifacts)
			}
			if len(sc.MemoryIDs) != tt.wantMemoryIDs {
				t.Errorf("MemoryIDs = %d, want %d", len(sc.MemoryIDs), tt.wantMemoryIDs)
			}
			if tt.unchangedParent && len(parent) != parentBefore {
				t.Errorf("parent slice mutated: %d msgs, want %d", len(parent), parentBefore)
			}
		})
	}
}

// TestBuildSpawnContext_SharedTranscriptCanaries asserts the SharedTranscript
// copy is a real copy (contains parent content including the canary).
func TestBuildSpawnContext_SharedTranscriptCanaries(t *testing.T) {
	parent := isolationParentMessages()
	sc := BuildSpawnContext(IsolationSharedTranscript, "brief", nil, nil, parent)

	if !strings.Contains(sc.Transcript[2].Content, isolationTestCanary) {
		t.Errorf("shared transcript should contain the canary (it is an explicit opt-in); got %q", sc.Transcript[2].Content)
	}
}

// TestBuildSpawnContext_UnknownEnumFailsClosed asserts unknown isolation
// values fail closed to ArtifactOnly: transcript empty, brief+artifacts kept.
// Each subtest resets the warn-once gate via isolationResetWarnOnce (guards
// against a sibling test file declaring the reset helper).
func TestBuildSpawnContext_UnknownEnumFailsClosed(t *testing.T) {
	parent := isolationParentMessages()
	artifacts := []ArtifactRef{{Path: "/tmp/a.txt", SHA256: "ff00"}}
	brief := "brief for unknown enum"

	for _, iso := range []ContextIsolation{"", "transcript", "SHARED", "artifact-only", "totally_bogus"} {
		t.Run(string(iso), func(t *testing.T) {
			isolationResetWarnOnce()

			sc := BuildSpawnContext(iso, brief, artifacts, nil, parent)

			if sc.Isolation != IsolationArtifactOnly {
				t.Errorf("unknown enum %q: Isolation = %q, want fail-closed %q", iso, sc.Isolation, IsolationArtifactOnly)
			}
			if len(sc.Transcript) != 0 {
				t.Errorf("unknown enum %q: Transcript must be empty, got %d messages", iso, len(sc.Transcript))
			}
			for _, msg := range sc.Transcript {
				if strings.Contains(msg.Content, isolationTestCanary) {
					t.Errorf("unknown enum %q: canary leaked into child transcript", iso)
				}
			}
			if sc.Brief != brief {
				t.Errorf("unknown enum %q: Brief = %q, want %q", iso, sc.Brief, brief)
			}
			if len(sc.Artifacts) != 1 || sc.Artifacts[0].Path != "/tmp/a.txt" {
				t.Errorf("unknown enum %q: artifacts not preserved: %+v", iso, sc.Artifacts)
			}
		})
	}
}

// TestBuildSpawnContext_ArtifactOnlyCanaryNeverLeaks is the leaf's Task-3
// canary: a parent transcript carrying PARENT_TOOL_DUMP_CANARY spawned under
// ArtifactOnly must not surface the canary anywhere in the child SpawnContext.
func TestBuildSpawnContext_ArtifactOnlyCanaryNeverLeaks(t *testing.T) {
	parent := isolationParentMessages()

	sc := BuildSpawnContext(IsolationArtifactOnly, "child brief", nil, []string{"mem-1"}, parent)

	if len(sc.Transcript) != 0 {
		t.Fatalf("ArtifactOnly Transcript must be empty, got %d messages", len(sc.Transcript))
	}
	for _, msg := range sc.Transcript {
		if strings.Contains(msg.Content, isolationTestCanary) {
			t.Fatal("canary leaked into child Transcript under ArtifactOnly")
		}
	}
	// The rendered brief block is what actually reaches the child's input;
	// assert the canary cannot ride along there either.
	rendered := RenderSpawnContext(sc)
	if strings.Contains(rendered, isolationTestCanary) {
		t.Error("canary leaked into rendered spawn brief under ArtifactOnly")
	}
	if !strings.Contains(rendered, "child brief") {
		t.Errorf("rendered brief missing Brief: %q", rendered)
	}
}

// TestBuildSpawnContext_NilParent asserts nil/empty parent inputs never panic
// and never fabricate transcript content.
func TestBuildSpawnContext_NilParent(t *testing.T) {
	for _, iso := range []ContextIsolation{IsolationArtifactOnly, IsolationSharedTranscript, IsolationBusMessage} {
		sc := BuildSpawnContext(iso, "b", nil, nil, nil)
		if len(sc.Transcript) != 0 {
			t.Errorf("%s: nil parent must yield empty transcript, got %d", iso, len(sc.Transcript))
		}
	}
}

// TestBuildSpawnContext_SharedTranscriptIsACopy asserts mutating the child's
// transcript slice does not touch the parent slice (aliasing hazard).
func TestBuildSpawnContext_SharedTranscriptIsACopy(t *testing.T) {
	parent := isolationParentMessages()
	sc := BuildSpawnContext(IsolationSharedTranscript, "b", nil, nil, parent)

	sc.Transcript[0].Content = "MUTATED"
	if parent[0].Content == "MUTATED" {
		t.Fatal("SharedTranscript transcript aliases the parent slice; must be a copy")
	}
}

// TestRenderSpawnContext_ArtifactRendering covers the artifact block of the
// rendered brief, with and without a hash.
func TestRenderSpawnContext_ArtifactRendering(t *testing.T) {
	sc := SpawnContext{
		Isolation: IsolationArtifactOnly,
		Brief:     "do the thing",
		Artifacts: []ArtifactRef{
			{Path: "/tmp/with.sha", SHA256: "deadbeef"},
			{Path: "/tmp/no.sha"},
		},
	}
	got := RenderSpawnContext(sc)
	for _, want := range []string{"do the thing", "## Prior Artifacts", "/tmp/with.sha (sha256: deadbeef)", "/tmp/no.sha"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered brief missing %q; got:\n%s", want, got)
		}
	}

	if got := RenderSpawnContext(SpawnContext{Isolation: IsolationArtifactOnly}); got != "" {
		t.Errorf("empty SpawnContext should render empty, got %q", got)
	}
}

// isolationResetWarnOnce replaces the package warn-once gate so unknown-enum
// subtests do not depend on execution order. It is test-only and must not be
// referenced from production code.
func isolationResetWarnOnce() {
	warnUnknownIsolationOnce = sync.Once{}
}

// --- Wiring tests: the three spawn sites ---

// TestPairSession_DefaultArtifactOnly asserts the pair create path defaults to
// artifact_only: no SharedTranscript flag → turn log excluded from prompts.
func TestPairSession_DefaultArtifactOnly(t *testing.T) {
	cfg := DefaultSessionConfig() // SharedTranscript unset (zero value)
	if cfg.SharedTranscript {
		t.Fatal("DefaultSessionConfig must not opt into shared transcripts")
	}

	sess := NewCollaborationSession("pair_programming", "task-1", []string{"coder", "reviewer"}, cfg)
	if sess.Isolation != IsolationArtifactOnly {
		t.Fatalf("default pair session isolation = %q, want artifact_only", sess.Isolation)
	}

	// Seed prior turns whose content acts as the canary.
	sess.AddTurn(TurnEntry{AgentID: "coder", Role: "driver", Content: isolationTestCanary + " raw turn dump"})

	d := NewPairProgrammingDriver(PairProgrammingDriverDeps{})
	prompt := d.buildDriverPrompt(sess, &PPConversation{SessionID: sess.ID}, "coder", "reviewer")
	if strings.Contains(prompt, isolationTestCanary) {
		t.Error("default pair driver prompt leaked turn-log canary; artifact_only expected")
	}
	if !strings.Contains(prompt, "## Task") {
		t.Errorf("driver prompt lost the task brief section:\n%s", prompt)
	}

	obsPrompt := d.buildObserverPrompt(sess, &PPConversation{SessionID: sess.ID}, "reviewer", "coder", "driver output")
	if strings.Contains(obsPrompt, isolationTestCanary) {
		t.Error("default observer prompt leaked turn-log canary; artifact_only expected")
	}
	// The observer still sees the driver's LATEST output (needed to review).
	if !strings.Contains(obsPrompt, "driver output") {
		t.Errorf("observer prompt must keep the driver's latest output:\n%s", obsPrompt)
	}
}

// TestPairSession_SharedTranscriptOptIn asserts the explicit flag routes the
// pair through shared_transcript, including the turn log in prompts.
func TestPairSession_SharedTranscriptOptIn(t *testing.T) {
	cfg := DefaultSessionConfig()
	cfg.SharedTranscript = true

	sess := NewCollaborationSession("pair_programming", "task-1", []string{"coder", "reviewer"}, cfg)
	if sess.Isolation != IsolationSharedTranscript {
		t.Fatalf("opt-in pair session isolation = %q, want shared_transcript", sess.Isolation)
	}

	sess.AddTurn(TurnEntry{AgentID: "coder", Role: "driver", Content: "shared transcript turn body"})

	d := NewPairProgrammingDriver(PairProgrammingDriverDeps{})
	prompt := d.buildDriverPrompt(sess, &PPConversation{SessionID: sess.ID}, "coder", "reviewer")
	if !strings.Contains(prompt, "shared transcript turn body") {
		t.Error("SharedTranscript opt-in prompt should include the turn log")
	}
}

// TestSessionConfig_SharedTranscriptTags pins the lowercase json/yaml tags so
// config files and API payloads round-trip (yaml-only structs silently emit Go
// field names when marshaled over JSON/RPC).
func TestSessionConfig_SharedTranscriptTags(t *testing.T) {
	cfgType := reflect.TypeOf(SessionConfig{})
	field, ok := cfgType.FieldByName("SharedTranscript")
	if !ok {
		t.Fatal("SessionConfig.SharedTranscript field missing")
	}
	if got := field.Tag.Get("json"); got != "shared_transcript,omitempty" {
		t.Errorf("json tag = %q, want %q", got, "shared_transcript,omitempty")
	}
	if got := field.Tag.Get("yaml"); got != "shared_transcript,omitempty" {
		t.Errorf("yaml tag = %q, want %q", got, "shared_transcript,omitempty")
	}
}

// TestDispatcherHandoff_SpawnContextIsArtifactOnly is the handoff canary: a
// parent carrying a tool-dump canary hands off to a child whose spawn context
// (and rendered brief) contains no transcript-derived content.
func TestDispatcherHandoff_SpawnContextIsArtifactOnly(t *testing.T) {
	parentMessages := isolationParentMessages() // carries PARENT_TOOL_DUMP_CANARY

	// Simulate the handoff hop exactly as buildHandoffSpawnContext does:
	// only the structured report-derived brief enters the child context.
	spawn := BuildSpawnContext(IsolationArtifactOnly, "prior agent report brief", []ArtifactRef{{Path: "/tmp/report.md"}}, nil, parentMessages)

	if len(spawn.Transcript) != 0 {
		t.Fatalf("handoff child got %d transcript messages; artifact_only must send none", len(spawn.Transcript))
	}
	for _, msg := range spawn.Transcript {
		if strings.Contains(msg.Content, isolationTestCanary) {
			t.Error("handoff child transcript contains parent tool-dump canary")
		}
	}
	rendered := RenderSpawnContext(spawn)
	if strings.Contains(rendered, isolationTestCanary) {
		t.Error("handoff child brief contains parent tool-dump canary")
	}
}

// TestBuildContextMessage_IncludesHandoffBrief pins the consumption side: the
// dispatcher prepends the structured Brief (never a transcript) to the child's
// input message.
func TestBuildContextMessage_IncludesHandoffBrief(t *testing.T) {
	d := &Dispatcher{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	result := &DispatchResult{
		Intent:        &Intent{Summary: "fix the bug"},
		OriginalInput: "fix the bug",
		Brief:         "accomplished: wrote failing test",
	}
	got := d.buildContextMessage(result, "conv-1")
	if !strings.Contains(got, "## Handoff Context") || !strings.Contains(got, "accomplished: wrote failing test") {
		t.Errorf("child input missing structured handoff brief:\n%s", got)
	}
	if !strings.Contains(got, "fix the bug") {
		t.Errorf("child input lost the original task:\n%s", got)
	}
}

// TestDelegateTask_SpawnBriefIsArtifactOnly is the subagent canary at the
// delegate boundary: the child's input carries the brief, and a parent
// transcript planted in the args cannot leak through. Mirrors the exact
// BuildSpawnContext call inside internal/tools/builtin DelegateTaskTool.
func TestDelegateTask_SpawnBriefIsArtifactOnly(t *testing.T) {
	spawn := BuildSpawnContext(IsolationArtifactOnly, "implement the parser", nil, nil, isolationParentMessages())

	full := RenderSpawnContext(spawn)
	if full != "implement the parser" {
		t.Errorf("delegate child input = %q, want the bare brief", full)
	}
	if strings.Contains(full, isolationTestCanary) {
		t.Error("delegate child input contains parent tool-dump canary")
	}
}

// TestWarnUnknownIsolationOnce asserts the warn-once gate is a real
// sync.Once (fire exactly once under repeated unknown-enum spawns).
func TestWarnUnknownIsolationOnce(t *testing.T) {
	isolationResetWarnOnce()

	const bogus = ContextIsolation("__bogus_warn_once__")
	for i := 0; i < 3; i++ {
		sc := BuildSpawnContext(bogus, "b", nil, nil, nil)
		if sc.Isolation != IsolationArtifactOnly {
			t.Fatalf("spawn %d: expected fail-closed isolation, got %q", i, sc.Isolation)
		}
	}

	isolationResetWarnOnce()
}
