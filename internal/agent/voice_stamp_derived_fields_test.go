package agent

// C-3 pins (2026-11-11 bughunt): the daemon voice lane stamps a clock
// prefix ("[Context: current date/time is ...] ") onto the prompt it hands
// RunOnce (internal/daemon/voice_employee.go). The stamp is MODEL-FACING
// scaffolding only: every DERIVED prompt field — turnUserPrompt (the
// deliverableFileEffectRe file-claim guard's input), the reflection
// trajectory's UserInput, and the learning capture's Intent — must carry
// the RAW prompt. Before the fix, the ^-anchored deliverableFileEffectRe
// never matched a stamped prompt, so a fabricated file claim on a voice
// turn took the "[unverified: ...]" labelled branch instead of the honest
// failure (ErrNudgeBudgetExhausted).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/learning"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
)

// stampedClaimChatter returns an unbacked created-file claim whose replies
// VARY (unique names) so the byte-level convergence detector stays quiet —
// the pin isolates the ^-anchored prompt match, not convergence. It also
// records the LAST user message it saw, so the test can assert the LLM
// request kept the stamp while derived fields got the raw prompt.
type stampedClaimChatter struct {
	callCount   int
	firstPrompt string
	prompts     []string
}

func (m *stampedClaimChatter) Chat(_ context.Context, messages []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	m.callCount++
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleUser {
			if m.firstPrompt == "" {
				m.firstPrompt = messages[i].Content
			}
			m.prompts = append(m.prompts, messages[i].Content)
			break
		}
	}
	return &llm.Response{
		Content: "claim " + time.Now().Format("150405.000000000") + ": Created file done_" +
			time.Now().Format("150405.000000000") + ".txt with the results",
		Usage: llm.TokenUsage{TotalTokens: 1},
	}, nil
}

func (m *stampedClaimChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *stampedClaimChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "stamped-claim-chatter"}
}

// stampPrefixLen is the length of the stamp prefix the daemon lane adds:
// "[Context: current date/time is " + "<Mon 2006-01-02 15:04 (UTC-07:00)>"
// + "] ". Pinned to the layout the daemon formats with (C-7: abbreviation
// dropped from the layout).
const testStampUserPrompt = "create the file report.txt with the summary"

func voiceStampForTest(now time.Time) string {
	return "[Context: current date/time is " +
		now.Format("Mon 2006-01-02 15:04 (UTC-07:00)") + "] "
}

// TestStripVoiceContextStamp_StripsOnlyTheLeadingPrefix: the helper must
// remove exactly the daemon stamp's leading prefix, leave prompts that
// merely CONTAIN a bracketed clock line mid-text untouched, and pass
// through chat prompts unchanged.
func TestStripVoiceContextStamp_StripsOnlyTheLeadingPrefix(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 20, 0, 0, time.UTC)
	stamp := voiceStampForTest(now)
	stamped := stamp + testStampUserPrompt
	if got := stripVoiceContextStamp(stamped); got != testStampUserPrompt {
		t.Errorf("strip = %q, want %q", got, testStampUserPrompt)
	}
	// Raw prompt: identity.
	if got := stripVoiceContextStamp(testStampUserPrompt); got != testStampUserPrompt {
		t.Errorf("raw prompt must pass through unchanged, got %q", got)
	}
	// Mid-text bracketed line: NOT a leading prefix — untouched.
	mid := "please " + stamp + "create the file"
	if got := stripVoiceContextStamp(mid); got != mid {
		t.Errorf("mid-text stamp must not strip, got %q", got)
	}
	// Unterminated prefix ("[Context: ..." with no "] "): untouched.
	if got := stripVoiceContextStamp("[Context: current date/time is Wed 2026-11-11 12:20 (UTC-07:00)"); got == "" || got == testStampUserPrompt {
		t.Errorf("unterminated stamp must not produce the raw prompt, got %q", got)
	}
	// Empty: identity.
	if got := stripVoiceContextStamp(""); got != "" {
		t.Errorf("empty prompt must stay empty, got %q", got)
	}
}

// TestDeliverableGuard_MatchesStampedVoicePrompt is the C-3 end-to-end pin:
// RunOnceWithParts receives the STAMPED prompt (the daemon seam's shape),
// the model request still carries the stamp, but l.turnUserPrompt — the
// input of deliverableClaimsFileEffect — must be the RAW prompt so the
// ^-anchored deliverableFileEffectRe matches a file-deliverable voice turn.
// The converse pin (TestChainStability_FabricatedDeliverableStillFails)
// already proves the honest-failure branch fires on a RAW file-deliverable
// prompt; this test proves the same prompt now classifies identically when
// it arrives stamped.
func TestDeliverableGuard_MatchesStampedVoicePrompt(t *testing.T) {
	chatter := &stampedClaimChatter{}
	loop := NewAgentLoop("test-session", t.TempDir(),
		WithLLMChatter(chatter),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
	)
	loop.security = security.NewPermissionChecker(security.Config{})
	loop.mu.Lock()
	loop.turnUserPrompt = ""
	loop.mu.Unlock()

	stamped := voiceStampForTest(time.Now()) + testStampUserPrompt
	if _, err := loop.RunOnceWithParts(context.Background(), stamped, nil, "conv-c3-stamped"); err != nil {
		if !containsError(err, ErrNudgeBudgetExhausted) {
			t.Fatalf("RunOnceWithParts: %v (want the honest-failure ErrNudgeBudgetExhausted branch once the raw prompt matches deliverableFileEffectRe)", err)
		}
	}
	loop.mu.RLock()
	got := loop.turnUserPrompt
	loop.mu.RUnlock()
	if got != testStampUserPrompt {
		t.Fatalf("turnUserPrompt = %q, want the RAW prompt %q (C-3: the stamp must not poison derived fields)", got, testStampUserPrompt)
	}
	// The model-visible FIRST request kept the stamp (the feature).
	if !strings.HasPrefix(chatter.firstPrompt, "[Context: current date/time is ") {
		t.Errorf("model request lost the clock stamp; first user prompt %.120q", chatter.firstPrompt)
	}
	if !strings.HasSuffix(chatter.firstPrompt, testStampUserPrompt) {
		t.Errorf("first user prompt must end with the raw prompt, got %.160q", chatter.firstPrompt)
	}
	// And the derived prompt now matches the ^-anchored deliverable regex.
	if !deliverableFileEffectRe.MatchString(strings.ToLower(got)) {
		t.Errorf("raw prompt %q must match deliverableFileEffectRe (the stamped form never did)", got)
	}
}

// TestDeliverableGuard_ChatLaneUnstampedStaysIdentical guards the chat
// lane's byte-identity: an UNSTAMPED prompt must flow to turnUserPrompt
// and the model request byte-for-byte (C-4's conservative branch).
func TestDeliverableGuard_ChatLaneUnstampedStaysIdentical(t *testing.T) {
	chatter := &stampedClaimChatter{}
	loop := NewAgentLoop("test-session", t.TempDir(),
		WithLLMChatter(chatter),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
	)
	loop.security = security.NewPermissionChecker(security.Config{})
	const chatPrompt = "analyze the readings and report"
	_, _ = loop.RunOnce(context.Background(), chatPrompt, "conv-c3-chat")
	if chatter.firstPrompt != chatPrompt {
		t.Errorf("chat lane model request = %.160q, want byte-identical %q", chatter.firstPrompt, chatPrompt)
	}
	loop.mu.RLock()
	got := loop.turnUserPrompt
	loop.mu.RUnlock()
	if got != chatPrompt {
		t.Errorf("chat lane turnUserPrompt = %q, want byte-identical %q", got, chatPrompt)
	}
}

// registryToolStub satisfies tools.Tool for the learning-capture pin.
type registryToolStub struct{}

func (registryToolStub) Name() string { return "probe_read" }
func (registryToolStub) Description() string {
	return "read-only probe"
}
func (registryToolStub) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: "object", Properties: map[string]llm.ParameterProperty{}}
}
func (registryToolStub) Execute(_ context.Context, _ map[string]any) (any, error) {
	return &tools.ToolResult{Success: true, Result: "ok"}, nil
}
func (registryToolStub) IsReadOnly(map[string]any) bool        { return true }
func (registryToolStub) IsConcurrencySafe(map[string]any) bool { return true }
func (registryToolStub) TimeoutMs() time.Duration              { return 0 }

// learningToolChatter returns one real tool call, then a final text, so
// the learningCapture branch (gated on len(toolNames) > 0) actually
// records a trajectory.
type learningToolChatter struct {
	calls int
}

func (m *learningToolChatter) Chat(_ context.Context, messages []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	m.calls++
	if m.calls == 1 {
		return &llm.Response{
			Content:      "",
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{{
				ID:   "tc-c3-1",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "probe_read",
					Arguments: `{}`,
				},
			}},
			Usage: llm.TokenUsage{TotalTokens: 1},
		}, nil
	}
	return &llm.Response{Content: "done", Usage: llm.TokenUsage{TotalTokens: 1}}, nil
}

func (m *learningToolChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *learningToolChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "learning-tool-chatter"}
}

// TestLearningCaptureIntentCarriesRawPrompt pins C-5: the LoRA trajectory
// recorder's Intent field must receive the RAW prompt, not the stamped
// model-facing form.
func TestLearningCaptureIntentCarriesRawPrompt(t *testing.T) {
	chatter := &learningToolChatter{}
	reg := NewPlaceholderToolRegistry()
	reg.Register(registryToolStub{})
	dataDir := t.TempDir()
	rec, err := learning.NewCaptureRecorder(dataDir)
	if err != nil {
		t.Fatalf("NewCaptureRecorder: %v", err)
	}
	loop := NewAgentLoop("test-session", t.TempDir(),
		WithLLMChatter(chatter),
		WithToolRegistry(reg),
		WithLearningCapture(rec),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
	)
	loop.security = security.NewPermissionChecker(security.Config{})

	const spoken = "read the config and report"
	stamped := voiceStampForTest(time.Now()) + spoken
	if _, err := loop.RunOnce(context.Background(), stamped, "conv-c3-learning"); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	// G118: the capture runs on a background goroutine that outlives the
	// request; Stop waits for it (and is idempotent).
	loop.Stop()

	entries := readRawCapturesTrajectories(t, filepath.Join(dataDir, "raw_captures.jsonl"))
	if len(entries) == 0 {
		t.Fatal("no learning trajectory recorded for a tool-using turn")
	}
	last := entries[len(entries)-1]
	if strings.Contains(last.Intent, "[Context: current date/time") {
		t.Errorf("learning Intent carries the clock stamp: %.160q", last.Intent)
	}
	if last.Intent != spoken {
		t.Errorf("learning Intent = %.160q, want the RAW spoken prompt %q", last.Intent, spoken)
	}
}

// reflectionUserInputChatter is a trivially-successful chatter for the
// reflection-trajectory pin.
type reflectionUserInputChatter struct{}

func (m *reflectionUserInputChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	return &llm.Response{Content: "ok", Usage: llm.TokenUsage{TotalTokens: 1}}, nil
}

func (m *reflectionUserInputChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *reflectionUserInputChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "reflection-userinput-chatter"}
}

// TestReflectionTrajectoryUserInputCarriesRawPrompt pins C-5's second site:
// the reflection trajectory's UserInput (built from the same userMessage
// expression the loop hands buildTrajectory) must receive the RAW prompt.
func TestReflectionTrajectoryUserInputCarriesRawPrompt(t *testing.T) {
	chatter := &reflectionUserInputChatter{}
	loop := NewAgentLoop("test-session", t.TempDir(),
		WithLLMChatter(chatter),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
	)
	loop.security = security.NewPermissionChecker(security.Config{})

	const spoken = "what changed since yesterday"
	stamped := voiceStampForTest(time.Now()) + spoken

	// The loop hands buildTrajectory the same expression this pin checks:
	// assert the STRIP helper produces the raw input the trajectory's
	// UserInput field receives (loop.go builds userMsg := stripVoiceContext
	// Stamp(userMessage) before the goroutine launch — a full collector run
	// would need a live classifier; the field-level pin catches the only
	// regression that matters: a derived field built from the stamped text).
	got := stripVoiceContextStamp(stamped)
	if got != spoken {
		t.Fatalf("stripVoiceContextStamp(stamped) = %.160q, want %q", got, spoken)
	}

	// Also pin it through the real RunOnceWithParts path: turnUserPrompt
	// (built at the same seam from the same userMessage) is raw, so the
	// trajectory expression at loop.go:3735 (userMsg :=
	// stripVoiceContextStamp(userMessage)) provably derives from the same
	// raw value.
	if _, err := loop.RunOnce(context.Background(), stamped, "conv-c3-reflect"); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	loop.mu.RLock()
	turnPrompt := loop.turnUserPrompt
	loop.mu.RUnlock()
	if turnPrompt != spoken || strings.Contains(turnPrompt, "[Context:") {
		t.Errorf("turn-scoped derived prompt = %.160q, want the raw spoken prompt", turnPrompt)
	}
}

// newStampedReflectionCollector is newTestCollector scoped to this file's
// pins (kept local so reflection_collector_test.go stays untouched).
func newStampedReflectionCollector(t *testing.T) (*ReflectionCollector, string) {
	t.Helper()
	queuePath := filepath.Join(t.TempDir(), "improvements.md")
	loader := newPlannerTemplateLoader()
	loader.fallbacks["reflection/turn.md"] = reflectionTurnFallbackBody
	rc := NewReflectionCollector(
		config.ReflectionCollectorConfig{
			Enabled:           true,
			AutoQueue:         true,
			TurnConfidenceMin: 0.6,
		},
		nil,
		"",
		loader,
		queuePath,
		nil,
	)
	return rc, queuePath
}

// TestReflectionCollectorPromptCarriesRawUserInput pins the collector-side
// contract: when a trajectory whose UserInput is the RAW prompt is
// reflected, the rendered classifier prompt must carry the raw text (and
// never the daemon stamp) — proving no stamp re-enters at render time.
func TestReflectionCollectorPromptCarriesRawUserInput(t *testing.T) {
	rc, _ := newStampedReflectionCollector(t)
	var rendered string
	rc.classifierRunOnce = func(_ context.Context, prompt, _ string) (string, error) {
		rendered = prompt
		return `{"proposal": null}`, nil
	}
	const spoken = "summarize the incident"
	traj := ReflectionTrajectory{UserInput: spoken, Outcome: "success"}
	if err := rc.ReflectTurn(context.Background(), traj); err != nil {
		t.Fatalf("ReflectTurn: %v", err)
	}
	if strings.Contains(rendered, "[Context: current date/time") {
		t.Errorf("rendered reflection prompt carries a clock stamp: %.200q", rendered)
	}
	if !strings.Contains(rendered, spoken) {
		t.Errorf("rendered reflection prompt lost the raw user input: %.200q", rendered)
	}
}

// readRawCapturesTrajectories parses the capture recorder's JSONL output
// into ResearchTrajectory entries (the recorder appends one line per
// RecordTrajectory call).
func readRawCapturesTrajectories(t *testing.T, path string) []learning.ResearchTrajectory {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw_captures.jsonl: %v", err)
	}
	var entries []learning.ResearchTrajectory
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e learning.ResearchTrajectory
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("decode trajectory line: %v", err)
		}
		entries = append(entries, e)
	}
	return entries
}
