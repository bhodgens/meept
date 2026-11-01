// golden.go - golden fixture capture (leaf 02 of the trajectory-golden-ci
// plan): turn a FakeLLM-scripted hermetic run into a committed C4 fixture
// and refuse everything that is not a scripted replay. See
// docs/plans/20261031-trajectory-golden-ci/.
package harness

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/eval/trajectorydiff"
	"github.com/caimlas/meept/internal/session"
)

// capturedFromLiteral is the ONLY value ever written to a fixture's
// captured_from field: goldens are captured exclusively from
// FakeLLM-scripted hermetic runs (policy R4). No code path in this file
// can write any other value.
const capturedFromLiteral = "fakellm-scripted"

// GoldenFixture is the C4 fixture format (frozen in the master plan):
// schema_version 1, captured_from the fakellm-scripted literal, match
// carrying the comparator options the consuming suite replays, golden
// the normalized reference sequence.
type GoldenFixture struct {
	SchemaVersion int              `json:"schema_version"`
	Scenario      string           `json:"scenario"`
	CapturedFrom  string           `json:"captured_from"`
	Match         GoldenMatchBlock `json:"match"`
	Golden        []GoldenEntry    `json:"golden"`
}

// GoldenMatchBlock mirrors trajectorydiff.Options in fixture form.
type GoldenMatchBlock struct {
	Mode        trajectorydiff.MatchMode          `json:"mode"`
	Threshold   float64                           `json:"threshold"`
	ArgRules    map[string]trajectorydiff.ArgRule `json:"arg_rules"`
	IgnoreTools []string                          `json:"ignore_tools"`
}

// Options rebuilds the comparator options the fixture carries. Empty
// mode/threshold are normalized to the comparator defaults at write time,
// so the round-trip is exact.
func (b GoldenMatchBlock) Options() trajectorydiff.Options {
	ignore := make(map[string]bool, len(b.IgnoreTools))
	for _, name := range b.IgnoreTools {
		ignore[name] = true
	}
	return trajectorydiff.Options{
		Mode:          b.Mode,
		Threshold:     b.Threshold,
		ArgRules:      b.ArgRules,
		IgnoreCallSet: ignore,
	}
}

// GoldenEntry is one golden step: tool name plus scrubbed argument shape
// (variable values replaced by the normalizer's <var> placeholder).
type GoldenEntry struct {
	Name      string         `json:"name"`
	ArgsShape map[string]any `json:"args_shape"`
}

// CaptureOptions configures CaptureGolden.
//
// Script MUST be non-nil: it is the exact FakeLLM script the consuming
// suite uses, and capture runs only through FakeLLM scripting. Goldens
// NEVER derive from live-model runs - a nil script is a refusal, as is
// a replay that records zero tool calls.
type CaptureOptions struct {
	ScenarioName string
	GoldenDir    string
	Script       func(f *FakeLLM)
	Match        trajectorydiff.Options
}

// CaptureGolden replays the scripted scenario end-to-end against a
// scratch daemon (the harness wires the scripted FakeLLM in as the
// daemon's sole model endpoint), pulls the recorded tool-call sequence
// for the assistant turn, normalizes it via trajectorydiff, and writes
// the frozen C4 fixture to <GoldenDir>/<ScenarioName>.golden.json.
// Returns the written path.
//
// Capture REFUSES (non-nil error, no file written) when:
//   - Script is nil or the scenario name is empty (non-scripted capture);
//   - the replay records zero tool calls (nothing to freeze);
//   - the recorded sequence cannot be normalized.
//
// The refusal paths never touch GoldenDir.
func CaptureGolden(t testing.TB, opts CaptureOptions) (string, error) {
	if opts.ScenarioName == "" {
		return "", fmt.Errorf("harness: capture golden: scenario name is empty; goldens capture scripted scenarios only")
	}
	if opts.Script == nil {
		return "", fmt.Errorf("harness: capture golden: scenario %q has no FakeLLM script; goldens NEVER derive from live-model or unscripted runs", opts.ScenarioName)
	}

	wrote, err := captureScriptedGolden(t, opts)
	if err != nil {
		return "", err
	}
	return wrote, nil
}

// captureScriptedGolden performs the scripted replay and fixture write.
// Split from CaptureGolden so the unscripted-refusal guard is the very
// first thing a reader sees.
func captureScriptedGolden(t testing.TB, opts CaptureOptions) (string, error) {
	// Replay: a scratch daemon whose model endpoint is the scripted
	// FakeLLM (Start() wires it in); the script closure programs it
	// before the turn runs.
	s := Start(t)
	opts.Script(s.Fake)

	s.RegisterProject(t, "golden-capture")
	sessionID := s.CreateSession(t, "golden-capture-"+opts.ScenarioName, s.ProjectDir)
	s.ChatTurn(t, sessionID, "Run the scripted golden-capture scenario for "+opts.ScenarioName, 120*time.Second)

	recorded, err := recordedSessionToolCalls(s, sessionID)
	if err != nil {
		return "", fmt.Errorf("harness: capture golden: read recorded calls: %w", err)
	}
	if len(recorded) == 0 {
		return "", fmt.Errorf("harness: capture golden: scenario %q recorded zero tool calls; refusing to freeze an empty trajectory", opts.ScenarioName)
	}

	normalized, err := trajectorydiff.NormalizeSessionCalls(recorded)
	if err != nil {
		return "", fmt.Errorf("harness: capture golden: normalize recorded calls: %w", err)
	}

	return writeGoldenFixture(opts.GoldenDir, opts.ScenarioName, opts.Match, normalized)
}

// recordedSessionToolCalls reads the replayed turn's tool-call sequence
// from the authoritative record: session_tool_calls ORDER BY seq
// (internal/session/store_sqlite.go GetToolCalls is the same query over
// the store) for the capture session, over the sessions.db in the
// scratch daemon's state dir. The DB opens READ-ONLY (the tasksdb.go
// DSN pattern) - capture never writes daemon state.
//
// The chat path does not persist session_tool_calls rows today (the
// store method exists; the loop-side writer is not wired), so when the
// session has zero persisted rows the sequence is read from the
// FakeLLM request log instead: the daemon echoes every assistant
// tool_calls message back to the model on the next completion request,
// so the LAST request's echoed set is the complete served sequence in
// serving order. That echo exists only inside a scripted FakeLLM run -
// the fake is the daemon's sole endpoint here - so the
// fakellm-scripted provenance guarantee holds either way. A capture
// whose turn served no tool calls yields zero calls from BOTH sources
// and is refused upstream.
func recordedSessionToolCalls(s *Stack, sessionID string) ([]session.ToolCall, error) {
	rows, err := readSessionToolCallRows(s, sessionID)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		calls := make([]session.ToolCall, 0, len(rows))
		for _, r := range rows {
			calls = append(calls, session.ToolCall{ToolName: r.name, Arguments: r.args, Seq: r.seq})
		}
		return calls, nil
	}
	return servedToolCallsFromRequestLog(s.Fake.Requests())
}

// sessionToolCallRow is one session_tool_calls row as read back.
type sessionToolCallRow struct {
	name string
	args string
	seq  int
}

// readSessionToolCallRows queries session_tool_calls joined to the
// capture session's messages, ORDER BY seq. A missing database or table
// (the daemon creates both lazily) is NOT an error - it degrades to the
// empty set so the request-log fallback can take over.
func readSessionToolCallRows(s *Stack, sessionID string) ([]sessionToolCallRow, error) {
	dbPath := filepath.Join(s.StateDir, "sessions.db")
	if _, err := os.Stat(dbPath); err != nil {
		return nil, nil //nolint:nilnil // absent DB degrades to the request-log fallback
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open sessions.db: %w", err)
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT tc.tool_name, tc.arguments, tc.seq
		FROM session_tool_calls tc
		JOIN session_messages m ON m.id = tc.message_id
		WHERE m.session_id = ?
		ORDER BY tc.seq`, sessionID)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil, nil //nolint:nilnil // absent table degrades to the request-log fallback
		}
		return nil, fmt.Errorf("query session_tool_calls: %w", err)
	}
	defer rows.Close()

	var out []sessionToolCallRow
	for rows.Next() {
		var row sessionToolCallRow
		if err := rows.Scan(&row.name, &row.args, &row.seq); err != nil {
			return nil, fmt.Errorf("scan tool call row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// servedToolCallsFromRequestLog reconstructs the served tool-call
// sequence from the FakeLLM request log: assistant messages echo the
// tool calls the daemon received back to the model on subsequent
// completion requests. The longest echo wins (it is the complete
// served set); calls deduplicate by tool_call ID keeping first-appearance
// order. Echo entries without an ID (an older client shape) deduplicate
// by name+arguments.
func servedToolCallsFromRequestLog(requests []map[string]any) ([]session.ToolCall, error) {
	type ledgerCall struct {
		id   string
		name string
		args string
	}
	var best []ledgerCall
	for _, body := range requests {
		msgs, _ := body["messages"].([]any)
		var echoed []ledgerCall
		seen := map[string]bool{}
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := msg["role"].(string); role != "assistant" {
				continue
			}
			tcs, ok := msg["tool_calls"].([]any)
			if !ok {
				continue
			}
			for _, tcAny := range tcs {
				tc, ok := tcAny.(map[string]any)
				if !ok {
					continue
				}
				fn, _ := tc["function"].(map[string]any)
				name, _ := fn["name"].(string)
				args, _ := fn["arguments"].(string)
				if name == "" {
					continue
				}
				id, _ := tc["id"].(string)
				key := id
				if key == "" {
					key = name + "\x00" + args
				}
				if seen[key] {
					continue
				}
				seen[key] = true
				echoed = append(echoed, ledgerCall{id: id, name: name, args: args})
			}
		}
		if len(echoed) > len(best) {
			best = echoed
		}
	}

	calls := make([]session.ToolCall, 0, len(best))
	for i, call := range best {
		calls = append(calls, session.ToolCall{
			ToolName:   call.name,
			Arguments:  call.args,
			ToolCallID: call.id,
			Seq:        i,
		})
	}
	return calls, nil
}

// writeGoldenFixture renders the C4 fixture bytes and writes them
// (0o644) to <GoldenDir>/<ScenarioName>.golden.json, creating GoldenDir
// when needed. It is the ONLY writer of fixture files in this file.
func writeGoldenFixture(goldenDir, scenario string, match trajectorydiff.Options, steps []trajectorydiff.NormalizedCall) (string, error) {
	if goldenDir == "" {
		return "", fmt.Errorf("harness: capture golden: golden dir is empty")
	}
	if err := os.MkdirAll(goldenDir, 0o755); err != nil {
		return "", fmt.Errorf("harness: capture golden: create golden dir: %w", err)
	}

	block := GoldenMatchBlock{
		Mode:      match.Mode,
		Threshold: match.Threshold,
		ArgRules:  match.ArgRules,
	}
	if block.Mode == "" {
		// Compare() defaults an empty mode to MatchInOrder; the fixture
		// records the EFFECTIVE options, not the zero values.
		block.Mode = trajectorydiff.MatchInOrder
	}
	if block.Threshold == 0 {
		block.Threshold = 1.0
	}
	if block.ArgRules == nil {
		block.ArgRules = map[string]trajectorydiff.ArgRule{}
	}
	block.IgnoreTools = make([]string, 0, len(match.IgnoreCallSet))
	for name := range match.IgnoreCallSet {
		block.IgnoreTools = append(block.IgnoreTools, name)
	}
	sort.Strings(block.IgnoreTools)

	fixture := GoldenFixture{
		SchemaVersion: 1,
		Scenario:      scenario,
		CapturedFrom:  capturedFromLiteral,
		Match:         block,
		Golden:        make([]GoldenEntry, 0, len(steps)),
	}
	for _, step := range steps {
		fixture.Golden = append(fixture.Golden, GoldenEntry{Name: step.Name, ArgsShape: step.ArgsShape})
	}

	out, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		return "", fmt.Errorf("harness: capture golden: marshal fixture: %w", err)
	}
	out = append(out, '\n')

	wrote := filepath.Join(goldenDir, scenario+".golden.json")
	if err := os.WriteFile(wrote, out, 0o644); err != nil {
		return "", fmt.Errorf("harness: capture golden: write fixture: %w", err)
	}
	return wrote, nil
}

// FixtureSteps rebuilds trajectorydiff.NormalizedCall values from a
// committed fixture for Compare (leaf 03's reader). InstanceID is
// recomputed as name#ordinal - the same rule NormalizeSessionCalls
// applies; fixtures do not carry ArgsRaw.
func FixtureSteps(fixture *GoldenFixture) []trajectorydiff.NormalizedCall {
	steps := make([]trajectorydiff.NormalizedCall, 0, len(fixture.Golden))
	ordinal := map[string]int{}
	for _, entry := range fixture.Golden {
		ord := ordinal[entry.Name]
		ordinal[entry.Name] = ord + 1
		steps = append(steps, trajectorydiff.NormalizedCall{
			Name:       entry.Name,
			ArgsShape:  entry.ArgsShape,
			InstanceID: fmt.Sprintf("%s#%d", entry.Name, ord),
		})
	}
	return steps
}

// LoadGoldenFixture reads and validates one committed fixture: schema
// version 1 and the captured_from literal are pinned - a fixture that
// fails either check is not a legal golden.
func LoadGoldenFixture(path string) (*GoldenFixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("harness: load golden fixture: %w", err)
	}
	var fixture GoldenFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		return nil, fmt.Errorf("harness: load golden fixture %s: %w", path, err)
	}
	if fixture.SchemaVersion != 1 {
		return nil, fmt.Errorf("harness: load golden fixture %s: schema_version %d, want 1", path, fixture.SchemaVersion)
	}
	if fixture.CapturedFrom != capturedFromLiteral {
		return nil, fmt.Errorf("harness: load golden fixture %s: captured_from %q, want %q", path, fixture.CapturedFrom, capturedFromLiteral)
	}
	return &fixture, nil
}
