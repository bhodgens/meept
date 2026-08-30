package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/skills"
)

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

// stateChatterStub is a scripted llmChatter: it records every prompt it is
// handed and returns the next scripted response, repeating the last one once
// the script is exhausted.
type stateChatterStub struct {
	responses []string
	calls     int
	prompts   [][]llm.ChatMessage
	chatErr   error
}

func (s *stateChatterStub) Chat(ctx context.Context, messages []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.chatErr != nil {
		return nil, s.chatErr
	}
	cp := make([]llm.ChatMessage, len(messages))
	copy(cp, messages)
	s.prompts = append(s.prompts, cp)
	idx := s.calls
	if idx >= len(s.responses) {
		idx = len(s.responses) - 1
	}
	s.calls++
	return &llm.Response{Content: s.responses[idx]}, nil
}

// stateRunnerStub records executed tool names and returns one fixed result.
type stateRunnerStub struct {
	executed []string
	result   *ExecutionResult
}

func (s *stateRunnerStub) run(_ context.Context, calls []llm.ToolCall) []*ExecutionResult {
	for _, c := range calls {
		s.executed = append(s.executed, c.Function.Name)
	}
	return []*ExecutionResult{s.result}
}

func stateTestSkill() *skills.Skill {
	return &skills.Skill{Name: "st-skill", Body: strings.Repeat("b", 100), MaxIterations: 10}
}

func stateTestRuntime(t *testing.T, cfg SkillStateConfig, ch llmChatter, runner *stateRunnerStub) (*SkillStateRuntime, *stateChatterStub, *stateRunnerStub) {
	t.Helper()
	loop := NewAgentLoop("ss-test", t.TempDir())
	r := NewSkillStateRuntime(loop, cfg, nil)
	chatterStub, ok := ch.(*stateChatterStub)
	if !ok {
		t.Fatalf("test misconfiguration: expected *stateChatterStub")
	}
	r.chatter = ch
	r.toolRunner = runner.run
	return r, chatterStub, runner
}

func toolResultOK(out string) *ExecutionResult {
	return &ExecutionResult{ToolCallID: "tc1", Success: true, Result: out}
}

// ---------------------------------------------------------------------------
// Task 2: schema + pure helpers
// ---------------------------------------------------------------------------

func TestDefaultStateSchema(t *testing.T) {
	schema := DefaultStateSchema()
	want := map[string]string{
		"files_touched": "array",
		"tests_run":     "array",
		"errors":        "string",
		"next_step":     "string",
	}
	if len(schema) != len(want) {
		t.Fatalf("schema len = %d, want %d", len(schema), len(want))
	}
	for _, f := range schema {
		if want[f.Name] != f.Type {
			t.Errorf("field %q type = %q, want %q", f.Name, f.Type, want[f.Name])
		}
	}
}

func TestMergeState_NullDeletesUnknownDropped(t *testing.T) {
	schema := DefaultStateSchema()
	old := map[string]any{"files_touched": []any{"a.go"}, "errors": "x", "rogue": 1}
	patch := map[string]any{
		"files_touched": []any{"a.go", "b.go"},
		"errors":        nil,    // delete
		"rogue2":        "nope", // unknown → dropped
	}
	clean, dropped := validateStatePatch(patch, schema)
	if _, ok := clean["rogue2"]; ok {
		t.Fatal("unknown key survived")
	}
	if len(dropped) != 1 || dropped[0] != "rogue2" {
		t.Fatalf("dropped: %v", dropped)
	}
	got := mergeState(old, clean, schema)
	if _, ok := got["errors"]; ok {
		t.Fatal("null did not delete")
	}
	ft, ok := got["files_touched"].([]any)
	if !ok || len(ft) != 2 {
		t.Fatalf("array not replaced: %#v", got["files_touched"])
	}
	if _, ok := got["rogue"]; ok {
		t.Fatal("pre-existing rogue key must be dropped from state too")
	}
}

// TestMergeState_MissingKeyUnchanged pins the paper §5.7 semantic: small
// models drop keys; a key absent from the patch must NOT delete the old value.
func TestMergeState_MissingKeyUnchanged(t *testing.T) {
	schema := DefaultStateSchema()
	old := map[string]any{
		"files_touched": []any{"a.go"},
		"tests_run":     []any{"t1"},
		"errors":        "boom",
		"next_step":     "keep going",
	}
	got := mergeState(old, map[string]any{"next_step": "read b.go"}, schema)
	if len(got["files_touched"].([]any)) != 1 {
		t.Fatalf("files_touched clobbered: %#v", got["files_touched"])
	}
	if len(got["tests_run"].([]any)) != 1 {
		t.Fatalf("tests_run clobbered: %#v", got["tests_run"])
	}
	if got["errors"] != "boom" {
		t.Fatalf("errors clobbered: %#v", got["errors"])
	}
	if got["next_step"] != "read b.go" {
		t.Fatalf("next_step not replaced: %#v", got["next_step"])
	}
	// merge must not mutate the old state.
	if _, ok := old["next_step"].(string); !ok || old["next_step"] == "read b.go" {
		t.Fatalf("mergeState mutated old state: %#v", old)
	}
}

func TestValidateStatePatch_WrongTypeDropped(t *testing.T) {
	schema := DefaultStateSchema()
	clean, dropped := validateStatePatch(map[string]any{
		"files_touched": "not-an-array",
		"errors":        42,
		"next_step":     "ok",
	}, schema)
	if _, ok := clean["files_touched"]; ok {
		t.Fatal("wrong-typed array accepted")
	}
	if _, ok := clean["errors"]; ok {
		t.Fatal("wrong-typed string accepted")
	}
	if clean["next_step"] != "ok" {
		t.Fatalf("valid string dropped: %#v", clean)
	}
	if len(dropped) != 2 {
		t.Fatalf("dropped = %v, want 2 entries", dropped)
	}
}

func TestBuildStatePrompt_Bounded(t *testing.T) {
	body := strings.Repeat("x", 100)
	state := map[string]any{}
	for i := 0; i < 50; i++ {
		state[fmt.Sprintf("k%d", i)] = strings.Repeat("v", 200)
	}
	state["files_touched"] = []any{"a.go"}
	obs := strings.Repeat("o", 80)
	maxChars := 200

	prompt := buildStatePrompt(body, state, obs, maxChars)

	if !strings.Contains(prompt, "CURRENT STATE") {
		t.Fatal("prompt missing CURRENT STATE marker")
	}
	if !strings.Contains(prompt, "LATEST OBSERVATION") {
		t.Fatal("prompt missing LATEST OBSERVATION marker")
	}
	if !strings.Contains(prompt, "...[truncated]") {
		t.Fatal("long values were not truncated")
	}
	// bound: body + capped Σ block (+ truncation marker slack) + observation
	// + fixed section overhead.
	overhead := 400
	if len(prompt) > len(body)+maxChars+len(obs)+overhead {
		t.Fatalf("prompt len = %d, want <= %d", len(prompt), len(body)+maxChars+len(obs)+overhead)
	}
	if !strings.Contains(prompt, obs) {
		t.Fatal("observation missing from prompt")
	}
}

// ---------------------------------------------------------------------------
// Task 3: Run step loop
// ---------------------------------------------------------------------------

func TestSkillStateRun_ToolThenAnswer(t *testing.T) {
	resp1 := `{"action":{"tool":"fs_read","args":{"path":"a.go"}},"state_patch":{"files_touched":["a.go"],"next_step":"read the other file"}}`
	resp2 := `{"action":{"answer":"all done"},"state_patch":{"tests_run":["t1"],"errors":null}}`
	ch := &stateChatterStub{responses: []string{resp1, resp2}}
	runner := &stateRunnerStub{result: toolResultOK("tool output 1")}
	r, _, _ := stateTestRuntime(t, SkillStateConfig{MaxStateChars: 2000, MaxIterations: 5}, ch, runner)

	answer, err := r.Run(context.Background(), stateTestSkill(), "task input", "conv-1")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "all done" {
		t.Fatalf("answer = %q, want %q", answer, "all done")
	}
	if len(runner.executed) != 1 || runner.executed[0] != "fs_read" {
		t.Fatalf("executed = %v, want exactly [fs_read]", runner.executed)
	}
	if len(ch.prompts) != 2 {
		t.Fatalf("chat calls = %d, want 2", len(ch.prompts))
	}

	first := promptsToString(ch.prompts[0])
	if !strings.Contains(first, strings.Repeat("b", 100)) {
		t.Fatal("first prompt missing skill body")
	}
	if !strings.Contains(first, "task input") {
		t.Fatal("first prompt missing O_0 input observation")
	}
	if !strings.Contains(first, "CURRENT STATE") || !strings.Contains(first, "LATEST OBSERVATION") {
		t.Fatal("first prompt missing section markers")
	}

	second := userPrompt(ch.prompts[1])
	// Reasoning discard: the raw model response content must not leak into
	// the next USER prompt. (The static system note legitimately mentions
	// "state_patch" in its patch-rules documentation, so scan the user
	// message only.)
	if strings.Contains(second, `"state_patch"`) {
		t.Fatalf("second user prompt contains raw model response (reasoning not discarded):\n%s", second)
	}
	if strings.Contains(second, `"path":"a.go"`) {
		t.Fatal("second prompt contains raw model response args")
	}
	// Patched state from resp1 IS visible in the second prompt.
	if !strings.Contains(second, "files_touched") || !strings.Contains(second, "a.go") {
		t.Fatal("second prompt missing patched files_touched")
	}
	// resp2's tests_run patch is applied after the FINAL chat call, so the
	// second prompt must still show the zero-valued tests_run.
	if !strings.Contains(second, `"tests_run":[]`) {
		t.Fatal("second prompt missing zero-valued tests_run")
	}
	// errors was explicitly deleted by resp2's patch — but resp2's patch is
	// applied at the final step, so it never renders in a prompt; only check
	// the observation feedback.
	if !strings.Contains(second, "tool output 1") {
		t.Fatal("second prompt missing tool observation")
	}
	if !strings.Contains(second, "read the other file") {
		t.Fatal("second prompt missing merged next_step")
	}
}

func TestSkillStateRun_MaxIterations(t *testing.T) {
	resp := `{"action":{"tool":"loop_tool","args":{}}}`
	ch := &stateChatterStub{responses: []string{resp}}
	runner := &stateRunnerStub{result: toolResultOK("out")}
	r, _, _ := stateTestRuntime(t, SkillStateConfig{MaxStateChars: 2000, MaxIterations: 3}, ch, runner)

	_, err := r.Run(context.Background(), stateTestSkill(), "task", "conv-1")
	if err == nil {
		t.Fatal("expected max-iterations error")
	}
	if !strings.Contains(err.Error(), "3") {
		t.Fatalf("error %q does not name the step count", err)
	}
	if len(runner.executed) != 3 {
		t.Fatalf("executed %d tools, want 3", len(runner.executed))
	}
}

func TestSkillStateRun_MalformedThenRetryThenAnswer(t *testing.T) {
	ch := &stateChatterStub{responses: []string{"this is not json at all", `{"action":{"answer":"recovered"}}`}}
	runner := &stateRunnerStub{result: toolResultOK("out")}
	r, _, _ := stateTestRuntime(t, SkillStateConfig{MaxStateChars: 2000, MaxIterations: 5}, ch, runner)

	answer, err := r.Run(context.Background(), stateTestSkill(), "task", "conv-1")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "recovered" {
		t.Fatalf("answer = %q", answer)
	}
	if len(ch.prompts) != 2 {
		t.Fatalf("chat calls = %d, want 2 (one retry)", len(ch.prompts))
	}
	// The retry must carry a corrective note and must not disturb Σ: the
	// second call still contains the zero-initialized state keys.
	second := promptsToString(ch.prompts[1])
	if !strings.Contains(second, "files_touched") {
		t.Fatal("retry prompt lost Σ state")
	}
}

func TestSkillStateRun_MalformedTwiceFails(t *testing.T) {
	ch := &stateChatterStub{responses: []string{"garbage one", "garbage two"}}
	runner := &stateRunnerStub{result: toolResultOK("out")}
	r, _, _ := stateTestRuntime(t, SkillStateConfig{MaxStateChars: 2000, MaxIterations: 5}, ch, runner)

	_, err := r.Run(context.Background(), stateTestSkill(), "task", "conv-1")
	if err == nil {
		t.Fatal("expected error after two malformed responses")
	}
	if len(ch.prompts) != 2 {
		t.Fatalf("chat calls = %d, want 2 (no second retry)", len(ch.prompts))
	}
}

func TestSkillStateRun_ToolErrorFeedsBack(t *testing.T) {
	resp1 := `{"action":{"tool":"bad_tool","args":{}}}`
	resp2 := `{"action":{"answer":"gave up"}}`
	ch := &stateChatterStub{responses: []string{resp1, resp2}}
	runner := &stateRunnerStub{result: &ExecutionResult{ToolCallID: "tc1", Success: false, Error: "boom"}}
	r, _, _ := stateTestRuntime(t, SkillStateConfig{MaxStateChars: 2000, MaxIterations: 5}, ch, runner)

	_, err := r.Run(context.Background(), stateTestSkill(), "task", "conv-1")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	second := promptsToString(ch.prompts[1])
	if !strings.Contains(second, "error: boom") {
		t.Fatalf("second prompt missing tool error observation:\n%s", second)
	}
}

func TestSkillStateRun_CtxCancelled(t *testing.T) {
	ch := &stateChatterStub{responses: []string{`{"action":{"answer":"late"}}`}}
	runner := &stateRunnerStub{result: toolResultOK("out")}
	r, _, _ := stateTestRuntime(t, SkillStateConfig{MaxStateChars: 2000, MaxIterations: 25}, ch, runner)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.Run(ctx, stateTestSkill(), "task", "conv-1")
	if err == nil {
		t.Fatal("expected ctx cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSkillStateRun_NilGuards(t *testing.T) {
	loop := NewAgentLoop("ss-nil", t.TempDir())
	r := NewSkillStateRuntime(loop, SkillStateConfig{}, nil)
	if _, err := r.Run(context.Background(), nil, "task", "conv"); err == nil {
		t.Fatal("expected error for nil skill")
	}
	r.chatter = &stateChatterStub{}
	if _, err := r.Run(context.Background(), stateTestSkill(), "task", "conv"); err == nil {
		t.Fatal("expected error for missing tool runner")
	}
}

// TestSkillStateRun_PatchAppliedMidRun proves patch semantics flow through
// the whole loop: resp1 patches tests_run + null-deletes errors; the SECOND
// prompt must show the merged Σ (t1 present, errors key gone).
func TestSkillStateRun_PatchAppliedMidRun(t *testing.T) {
	resp1 := `{"action":{"tool":"t","args":{}},"state_patch":{"tests_run":["t1"],"errors":null,"next_step":"finish"}}`
	resp2 := `{"action":{"answer":"done"}}`
	ch := &stateChatterStub{responses: []string{resp1, resp2}}
	runner := &stateRunnerStub{result: toolResultOK("out")}
	r, _, _ := stateTestRuntime(t, SkillStateConfig{MaxStateChars: 2000, MaxIterations: 5}, ch, runner)

	if _, err := r.Run(context.Background(), stateTestSkill(), "task", "conv-1"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	second := userPrompt(ch.prompts[1])
	if !strings.Contains(second, `"tests_run":["t1"]`) {
		t.Fatalf("second prompt missing merged tests_run:\n%s", second)
	}
	if strings.Contains(second, `"errors"`) {
		t.Fatalf("errors key not deleted in second prompt:\n%s", second)
	}
	if !strings.Contains(second, "finish") {
		t.Fatalf("second prompt missing merged next_step:\n%s", second)
	}
}

// ---------------------------------------------------------------------------
// Trace wiring (nil-safe)
// ---------------------------------------------------------------------------

type traceWriterCapture struct {
	recs []*traceRecordMirror
	err  error
}

func (c *traceWriterCapture) WriteTrace(rec *traceRecordMirror) (string, error) {
	c.recs = append(c.recs, rec)
	return "tr-1", c.err
}

func TestSkillStateRun_TraceWritten(t *testing.T) {
	resp1 := `{"action":{"tool":"fs_read","args":{}}}`
	resp2 := `{"action":{"answer":"done"}}`
	ch := &stateChatterStub{responses: []string{resp1, resp2}}
	runner := &stateRunnerStub{result: toolResultOK("out")}
	r, _, _ := stateTestRuntime(t, SkillStateConfig{MaxStateChars: 2000, MaxIterations: 5}, ch, runner)
	tw := &traceWriterCapture{}
	r.traceWriter = tw

	if _, err := r.Run(context.Background(), stateTestSkill(), "task input", "conv-9"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(tw.recs) != 1 {
		t.Fatalf("trace records = %d, want 1", len(tw.recs))
	}
	rec := tw.recs[0]
	if rec.SessionID != "conv-9" {
		t.Fatalf("session id = %q", rec.SessionID)
	}
	if rec.Outcome != "success" {
		t.Fatalf("outcome = %q", rec.Outcome)
	}
	if len(rec.InjectedSkills) != 1 || rec.InjectedSkills[0] != "st-skill" {
		t.Fatalf("injected skills = %v", rec.InjectedSkills)
	}
	if len(rec.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(rec.Steps))
	}
	for _, s := range rec.Steps {
		if s.Action != "state_step" {
			t.Fatalf("step action = %q, want state_step", s.Action)
		}
		if s.Input == "" {
			t.Fatal("step input (prompt digest) empty")
		}
	}
	if rec.Steps[0].Output != "task input" {
		t.Fatalf("step 0 output = %q, want O_0", rec.Steps[0].Output)
	}
	if rec.Steps[1].Output != "out" {
		t.Fatalf("step 1 output = %q, want tool observation", rec.Steps[1].Output)
	}
	if rec.Summary != "done" {
		t.Fatalf("summary = %q", rec.Summary)
	}
}

func TestSkillStateRun_TraceFailureRecorded(t *testing.T) {
	ch := &stateChatterStub{responses: []string{"garbage", "garbage"}}
	runner := &stateRunnerStub{result: toolResultOK("out")}
	r, _, _ := stateTestRuntime(t, SkillStateConfig{MaxStateChars: 2000, MaxIterations: 5}, ch, runner)
	tw := &traceWriterCapture{}
	r.traceWriter = tw

	if _, err := r.Run(context.Background(), stateTestSkill(), "task", "conv-1"); err == nil {
		t.Fatal("expected error")
	}
	if len(tw.recs) != 1 {
		t.Fatalf("trace records = %d, want 1", len(tw.recs))
	}
	if tw.recs[0].Outcome != "failure" {
		t.Fatalf("outcome = %q, want failure", tw.recs[0].Outcome)
	}
	if tw.recs[0].Error == "" {
		t.Fatal("failure record missing error text")
	}
}

func TestSkillStateRun_TraceNilSafe(t *testing.T) {
	ch := &stateChatterStub{responses: []string{`{"action":{"answer":"ok"}}`}}
	runner := &stateRunnerStub{result: toolResultOK("out")}
	r, _, _ := stateTestRuntime(t, SkillStateConfig{MaxStateChars: 2000, MaxIterations: 5}, ch, runner)
	if r.traceWriter != nil {
		t.Fatal("default runtime must have nil trace writer")
	}
	if _, err := r.Run(context.Background(), stateTestSkill(), "task", "conv-1"); err != nil {
		t.Fatalf("Run with nil trace writer: %v", err)
	}
}

// promptsToString concatenates the contents of a captured message list.
func promptsToString(msgs []llm.ChatMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// userPrompt returns the LAST user-role message's content from a captured
// message list (the per-step skill prompt proper).
func userPrompt(msgs []llm.ChatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}
