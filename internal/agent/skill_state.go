package agent

// SKILL.state runtime core (arXiv:2608.26263 §3 runtime, §5.7 error
// taxonomy, §7 limitations).
//
// State mode executes a skill with an explicit Σ state object instead of the
// append-only conversation: per step the model sees ONLY the skill body (P),
// the current Σ JSON, and the latest observation (O_t). The model returns a
// strict-JSON action plus a state patch; the runtime validates the patch
// against a fixed schema, merges it (missing key = unchanged, explicit null =
// delete, unknown key = dropped+reported), executes the action, and feeds the
// tool result back as the next observation. Intermediate reasoning never
// persists into the next prompt.
//
// The runtime is free of conversation-store access: Σ is the only carrier.
// Nothing activates without leaf 05's config + flag.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/util/markdown"
)

// ---------------------------------------------------------------------------
// Schema
// ---------------------------------------------------------------------------

// StateField describes one Σ key. v1 ships exactly the default coding schema
// (arXiv:2608.26263 §3.1: one schema per domain, authored once in code).
type StateField struct {
	Name string // files_touched | tests_run | errors | next_step
	Type string // "array" | "string"
	Desc string
}

// DefaultStateSchema returns the default coding-task Σ schema:
// files_touched(array), tests_run(array), errors(string), next_step(string).
func DefaultStateSchema() []StateField {
	return []StateField{
		{Name: "files_touched", Type: "array", Desc: "file paths modified or read so far"},
		{Name: "tests_run", Type: "array", Desc: "test commands or names executed so far"},
		{Name: "errors", Type: "string", Desc: "the current blocking error, empty when none"},
		{Name: "next_step", Type: "string", Desc: "what to do next"},
	}
}

// ---------------------------------------------------------------------------
// Pure patch helpers
// ---------------------------------------------------------------------------

// validateStatePatch checks patch keys against schema and drops unknown keys
// and wrong-typed values, returning the clean patch and the bare names of
// what was dropped (reported to the model-side log by the caller — this
// helper stays pure). Explicit null values pass validation here so
// mergeState can apply deletion; wrong-typed values are dropped.
func validateStatePatch(patch map[string]any, schema []StateField) (clean map[string]any, dropped []string) {
	clean = make(map[string]any, len(patch))
	types := make(map[string]string, len(schema))
	for _, f := range schema {
		types[f.Name] = f.Type
	}
	for k, v := range patch {
		want, known := types[k]
		if !known {
			dropped = append(dropped, k)
			continue
		}
		if v == nil {
			// Explicit null = deletion request; keep for mergeState.
			clean[k] = nil
			continue
		}
		switch want {
		case "array":
			if _, ok := v.([]any); !ok {
				dropped = append(dropped, k)
				continue
			}
		case "string":
			if _, ok := v.(string); !ok {
				dropped = append(dropped, k)
				continue
			}
		}
		clean[k] = v
	}
	return clean, dropped
}

// mergeState applies clean (already-validated) patch semantics to old and
// returns a NEW map: replace array/string values; explicit null DELETES the
// key; a missing key means unchanged (paper §5.7: small models drop keys —
// missing must NOT delete). The result is total over the schema: every schema
// key present, and any pre-existing keys outside the schema are dropped.
func mergeState(old map[string]any, patch map[string]any, schema []StateField) map[string]any {
	next := make(map[string]any, len(schema))
	for _, f := range schema {
		if v, ok := patch[f.Name]; ok {
			if v == nil {
				continue // explicit null deletes
			}
			next[f.Name] = v
			continue
		}
		if v, ok := old[f.Name]; ok {
			next[f.Name] = v
			continue
		}
		next[f.Name] = zeroStateValue(f.Type)
	}
	return next
}

// zeroStateValue returns the zero value for a schema type: nil slice for
// "array", empty string for "string".
func zeroStateValue(typ string) any {
	if typ == "array" {
		return []any{}
	}
	return ""
}

// initialSkillState builds the zero-valued Σ: all schema keys present with
// zero values so merges are total.
func initialSkillState(schema []StateField) map[string]any {
	return mergeState(nil, nil, schema)
}

// ---------------------------------------------------------------------------
// Prompt construction
// ---------------------------------------------------------------------------

const (
	// stateValueCharCap caps each rendered Σ value before truncation.
	stateValueCharCap = 400
	// stateTruncMarker is appended to truncated values.
	stateTruncMarker = "...[truncated]"
)

// statePromptSection markers are fixed so tests and the trace log can find
// the section boundaries.
const (
	stateSectionState = "CURRENT STATE"
	stateSectionObs   = "LATEST OBSERVATION"
)

// buildStatePrompt renders the per-step prompt: skill body (P), the Σ JSON
// block in schema key order, then the latest observation. The Σ block is
// capped at maxChars total (plus the truncation marker); each value is capped
// at stateValueCharCap.
func buildStatePrompt(skillBody string, state map[string]any, observation string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = defaultSkillStateCfg.MaxStateChars
	}

	var b strings.Builder
	b.WriteString(strings.TrimSpace(skillBody))
	b.WriteString("\n\n")
	b.WriteString(stateSectionState)
	b.WriteString("\n")
	b.WriteString(renderStateJSON(state, maxChars))
	b.WriteString("\n\n")
	b.WriteString(stateSectionObs)
	b.WriteString("\n")
	b.WriteString(observation)
	return b.String()
}

// renderStateJSON renders Σ as compact JSON within maxChars total. Keys are
// emitted in schema order first (each schema key present on state is emitted
// with a guaranteed minimal entry while budget remains), then any remaining
// keys in sorted order while budget lasts. The Σ block NEVER exceeds
// maxChars, with truncation honestly marked.
func renderStateJSON(state map[string]any, maxChars int) string {
	if state == nil {
		state = map[string]any{}
	}
	schema := DefaultStateSchema()

	var b strings.Builder
	b.WriteByte('{')
	// Deterministic emission: schema order first, then extras sorted.
	var extras []string
	for k := range state {
		if !isSchemaKey(k, schema) {
			extras = append(extras, k)
		}
	}
	sortStrings(extras)
	ordered := make([]string, 0, len(schema)+len(extras))
	for _, f := range schema {
		if _, ok := state[f.Name]; ok {
			ordered = append(ordered, f.Name)
		}
	}
	ordered = append(ordered, extras...)

	budget := maxChars
	first := true
	for _, k := range ordered {
		keyJSON, _ := json.Marshal(k)
		fixed := len(keyJSON) + 2 // ':' plus ',' when not first
		if budget-fixed < len(stateTruncMarker)+3 {
			break
		}
		rendered := renderStateValue(state[k], budget-fixed)
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.Write(keyJSON)
		b.WriteByte(':')
		b.Write(rendered)
		budget -= fixed + len(rendered)
	}
	b.WriteByte('}')
	return b.String()
}

// isSchemaKey reports whether k names a schema field.
func isSchemaKey(k string, schema []StateField) bool {
	for _, f := range schema {
		if f.Name == k {
			return true
		}
	}
	return false
}

// renderStateValue renders one Σ value as valid JSON within cap bytes
// (caller guarantees cap > marker + quotes). Strings are first capped at
// stateValueCharCap with the truncation marker; arrays keep whole elements
// while they fit and end with a marker element when elements were dropped.
// Output is always valid JSON.
func renderStateValue(val any, capBytes int) []byte {
	switch v := val.(type) {
	case string:
		s := v
		if len(s) > stateValueCharCap {
			s = s[:stateValueCharCap] + stateTruncMarker
		}
		return marshalWithin(s, capBytes)
	case []any:
		var b strings.Builder
		b.WriteByte('[')
		used := 2
		first := true
		dropped := false
		for _, e := range v {
			ej, err := json.Marshal(e)
			if err != nil {
				ej = []byte(`null`)
			}
			need := len(ej) + 1
			if used+need+len(stateTruncMarker)+3 > capBytes {
				dropped = true
				break
			}
			if !first {
				b.WriteByte(',')
			}
			first = false
			b.Write(ej)
			used += need
		}
		if dropped || used+1 > capBytes-len(`]`) {
			// room for the marker element was reserved above
			if !first {
				b.WriteByte(',')
			}
			mj, _ := json.Marshal(stateTruncMarker)
			b.Write(mj)
		}
		b.WriteByte(']')
		return []byte(b.String())
	default:
		return marshalWithin(val, capBytes)
	}
}

// marshalWithin marshals val; if the result exceeds capBytes it falls back to
// a truncated marker string that always fits.
func marshalWithin(val any, capBytes int) []byte {
	b, err := json.Marshal(val)
	if err != nil {
		b = []byte(`null`)
	}
	if len(b) <= capBytes {
		return b
	}
	mj, _ := json.Marshal(stateTruncMarker)
	return mj
}

// sortStrings sorts in place (small helper to keep this file free of imports
// beyond stdlib + the repo ones above; sort.Slice would do the same).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// ---------------------------------------------------------------------------
// Wire shapes + response parsing
// ---------------------------------------------------------------------------

// stateAction mirrors the frozen model wire shape:
//
//	{"action": {"tool": "<name>", "args": {...}}}
//	{"action": {"answer": "<final text>"}, "state_patch": {...}}
type stateAction struct {
	Tool       string         `json:"tool"`
	Answer     string         `json:"answer"`
	Args       map[string]any `json:"args"`
	StatePatch map[string]any `json:"state_patch"`
}

// stateResponse is the strict-JSON model response envelope.
type stateResponse struct {
	Action     *stateAction   `json:"action"`
	StatePatch map[string]any `json:"state_patch"`
}

// stateGBNFGrammar is the GBNF grammar for the response shape, attached only
// when llm.GBNFConstrainedEnabled(). It allows the two action forms with an
// optional state_patch. Kept permissive on value contents (strings may hold
// any characters; args may hold any JSON object).
const stateGBNFGrammar = `root ::= "{" ws "\"action\"" ws ":" ws action ws ("," ws "\"state_patch\"" ws ":" ws object)? ws "}"
action ::= "{" ws "\"answer\"" ws ":" ws string ws "}" | "{" ws "\"tool\"" ws ":" ws string ws ("," ws "\"args\"" ws ":" ws object)? ws "}"
object ::= "{" ws (string ws ":" ws value (ws "," ws string ws ":" ws value)*)? ws "}"
value ::= string | number | object | array | "true" | "false" | "null"
array ::= "[" ws (value (ws "," ws value)*)? ws "]"
number ::= "-"? ([0-9] | [1-9] [0-9]*) ("." [0-9]+)? ([eE] [-+]? [0-9]+)?
string ::= "\"" ( [^"\\] | "\\" . )* "\""
ws ::= [ \t\n\r]*`

// stateSystemNote is the fixed system instruction prepended to every call.
const stateSystemNote = `You are executing a procedural skill step by step. Respond with STRICT JSON only — no prose, no markdown fences. Allowed shapes:
{"action": {"tool": "<name>", "args": {…}}, "state_patch": {…}}
{"action": {"answer": "<final text>"}, "state_patch": {…}}
state_patch rules: arrays/strings replace the current value; "key": null DELETES the key; omitting a key leaves it unchanged; unknown keys are rejected.`

// parseStateResponse extracts and validates a model response. It returns an
// error when the payload is not the strict shape (tool or answer action).
func parseStateResponse(content string) (*stateResponse, error) {
	data := markdown.ExtractJSON(content)
	if len(data) == 0 {
		return nil, errors.New("state runtime: no JSON found in response")
	}
	var resp stateResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("state runtime: response is not valid JSON: %w", err)
	}
	if resp.Action == nil {
		return nil, errors.New("state runtime: response missing action object")
	}
	switch {
	case resp.Action.Answer != "":
	case resp.Action.Tool != "":
		if resp.Action.Args == nil {
			resp.Action.Args = map[string]any{}
		}
	default:
		return nil, errors.New("state runtime: action must set tool or answer")
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// Runtime
// ---------------------------------------------------------------------------

// llmChatter is the narrow interface the state runtime needs for LLM calls —
// the same shape as internal/skills/lifecycle's llmChatter so both *llm.Client
// and llm.Chatter satisfy it and tests can inject a stub.
type llmChatter interface {
	Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error)
}

// stateToolRunner executes exactly the tool calls the runtime hands it. It is
// injected (rather than constructed) so the runtime never builds a real
// Executor; leaf 05 wires it from the loop's executor.
type stateToolRunner func(ctx context.Context, toolCalls []llm.ToolCall) []*ExecutionResult

// SkillStateConfig bounds Σ rendering and the step loop.
type SkillStateConfig struct {
	MaxStateChars int // prompt budget for the Σ JSON block; default 2000
	MaxIterations int // step cap; default 25
}

// defaultSkillStateCfg mirrors the contract defaults.
var defaultSkillStateCfg = SkillStateConfig{MaxStateChars: 2000, MaxIterations: 25}

// SkillStateRuntime executes a skill in SKILL.state mode.
type SkillStateRuntime struct {
	loop        *AgentLoop
	cfg         SkillStateConfig
	logger      *slog.Logger
	chatter     llmChatter
	toolRunner  stateToolRunner
	traceWriter TraceWriter // nil-safe; leaf 01's loop writer when wired
}

// NewSkillStateRuntime builds a runtime bound to loop. The LLM chatter is
// taken from the loop's client when available; the tool runner and trace
// writer are injected separately (WithToolRunner / WithTraceWriterInjection,
// or direct field assignment by leaf 05 — all nil-guarded at Run).
func NewSkillStateRuntime(loop *AgentLoop, cfg SkillStateConfig, logger *slog.Logger) *SkillStateRuntime {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MaxStateChars <= 0 {
		cfg.MaxStateChars = defaultSkillStateCfg.MaxStateChars
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = defaultSkillStateCfg.MaxIterations
	}
	r := &SkillStateRuntime{
		loop:   loop,
		cfg:    cfg,
		logger: logger,
	}
	if loop != nil {
		if loop.llm != nil {
			r.chatter = loop.llm
		} else if loop.llmClient != nil {
			r.chatter = loop.llmClient
		}
	}
	return r
}

// WithToolRunner injects the tool execution seam.
func (r *SkillStateRuntime) WithToolRunner(runner stateToolRunner) *SkillStateRuntime {
	r.toolRunner = runner
	return r
}

// WithStateTraceWriter injects the leaf-01 trace writer (nil-safe).
func (r *SkillStateRuntime) WithStateTraceWriter(tw TraceWriter) *SkillStateRuntime {
	r.traceWriter = tw
	return r
}

// stateStepMaxChars bounds the trace-record prompt digest.
const stateStepMaxChars = 200

// Run executes skill in state mode (paper Algorithm 1): per step build the
// prompt → chat → parse (malformed ⇒ ONE corrective retry, then error) →
// validate + merge Σ → execute tool or return answer. Terminates on an
// answer action, when MaxIterations steps are exhausted (error naming the
// step count), or when ctx is cancelled.
func (r *SkillStateRuntime) Run(ctx context.Context, skill *skills.Skill, input, conversationID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("state runtime: %w", err)
	}
	if skill == nil {
		return "", errors.New("state runtime: nil skill")
	}
	if r.chatter == nil {
		return "", errors.New("state runtime: no LLM chatter available")
	}
	if r.toolRunner == nil {
		return "", errors.New("state runtime: no tool runner injected")
	}

	maxIters := r.cfg.MaxIterations
	if skill.MaxIterations > 0 && skill.MaxIterations < maxIters {
		maxIters = skill.MaxIterations
	}
	// Σ starts zero-valued over the whole schema.
	state := initialSkillState(DefaultStateSchema())
	schema := DefaultStateSchema()
	observation := input // O_0

	steps := make([]traceStepMirror, 0, maxIters)
	answer := ""
	var runErr error

	for step := 0; step < maxIters; step++ {
		if err := ctx.Err(); err != nil {
			runErr = fmt.Errorf("state runtime: %w", err)
			break
		}

		prompt := buildStatePrompt(skill.Body, state, observation, r.cfg.MaxStateChars)
		messages := []llm.ChatMessage{
			{Role: llm.RoleSystem, Content: stateSystemNote},
			{Role: llm.RoleUser, Content: prompt},
		}

		var opts []llm.ChatOption
		if llm.GBNFConstrainedEnabled() {
			opts = append(opts, llm.WithRawGrammar(stateGBNFGrammar))
		}

		resp, err := r.chatter.Chat(ctx, messages, opts...)
		if err != nil {
			runErr = fmt.Errorf("state runtime: step %d chat: %w", step+1, err)
			break
		}

		parsed, perr := parseStateResponse(resp.Content)
		if perr != nil {
			// ONE corrective retry (paper §7 rollback-retry); Σ untouched.
			messages = append(messages,
				llm.ChatMessage{Role: llm.RoleAssistant, Content: resp.Content},
				llm.ChatMessage{Role: llm.RoleUser, Content: "Your previous response was malformed. " +
					perr.Error() + " Respond again with STRICT JSON only in the documented shape."},
			)
			resp, err = r.chatter.Chat(ctx, messages, opts...)
			if err != nil {
				runErr = fmt.Errorf("state runtime: step %d chat: %w", step+1, err)
				break
			}
			parsed, perr = parseStateResponse(resp.Content)
			if perr != nil {
				runErr = fmt.Errorf("state runtime: step %d malformed response twice: %w", step+1, perr)
				break
			}
		}

		// Validate + merge the patch BEFORE acting, so Σ never reflects a
		// partially-executed step. Dropped keys are logged, never fatal.
		clean, dropped := validateStatePatch(parsed.StatePatch, schema)
		for _, d := range dropped {
			r.logger.Debug("state runtime: dropped patch key", "key", d)
		}
		state = mergeState(state, clean, schema)

		// Trace the step with the observation it CONSUMED (O_t): record
		// before acting so the step entry stays aligned with its prompt.
		steps = append(steps, traceStepMirror{
			Action:  "state_step",
			Input:   truncateForTrace(prompt),
			Output:  observation,
			Success: true,
		})

		if parsed.Action.Answer != "" {
			answer = parsed.Action.Answer
			break
		}

		// Tool action: build exactly one ToolCall for the injected runner.
		argsJSON, aerr := json.Marshal(parsed.Action.Args)
		if aerr != nil {
			runErr = fmt.Errorf("state runtime: step %d marshal args: %w", step+1, aerr)
			break
		}
		call := llm.ToolCall{
			ID:   fmt.Sprintf("state-%d-%s", step+1, parsed.Action.Tool),
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      parsed.Action.Tool,
				Arguments: string(argsJSON),
			},
		}
		results := r.toolRunner(ctx, []llm.ToolCall{call})
		if len(results) == 0 {
			runErr = fmt.Errorf("state runtime: step %d tool %q returned no result", step+1, parsed.Action.Tool)
			break
		}
		res := results[0]
		if res != nil && res.Success {
			observation = fmt.Sprintf("%v", res.Result)
		} else if res != nil {
			observation = "error: " + res.Error
		} else {
			observation = "error: tool returned nil result"
		}
	}

	if runErr == nil && answer == "" {
		runErr = fmt.Errorf("state runtime: reached max iterations (%d steps) without an answer", maxIters)
	}

	r.writeStateTrace(conversationID, skill.Name, steps, answer, runErr)
	if runErr != nil {
		return answer, runErr
	}
	return answer, nil
}

// writeStateTrace appends ONE trace record per Run when a TraceWriter is
// wired; write errors are logged at Debug only.
func (r *SkillStateRuntime) writeStateTrace(conversationID, skillName string, steps []traceStepMirror, summary string, runErr error) {
	if r.traceWriter == nil {
		return
	}
	rec := &traceRecordMirror{
		SessionID:      conversationID,
		InjectedSkills: []string{skillName},
		Steps:          steps,
		Summary:        summary,
	}
	if runErr != nil {
		rec.Outcome = "failure"
		rec.Error = runErr.Error()
	} else {
		rec.Outcome = "success"
	}
	if _, err := r.traceWriter.WriteTrace(rec); err != nil {
		r.logger.Debug("state runtime: trace write failed", "error", err)
	}
}

// truncateForTrace bounds a prompt digest for the trace record.
func truncateForTrace(s string) string {
	if len(s) <= stateStepMaxChars {
		return s
	}
	return s[:stateStepMaxChars] + stateTruncMarker
}
