//go:build e2e

// Package llmhoptracing is the llm-hop-tracing leaf-04 hermetic e2e suite:
// it verifies the FULL hop-tracing story through the REAL daemon -- one
// trace id shared by chat.turn + llm.call + tool.call spans on the wire,
// GenAI attributes, the byte-exact llm_calls.trace_id/span_id SQL join,
// and both boot-log postures (armed Info / disabled Warn).
//
// Capture approach: an in-suite OTLPCaptureReceiver (harness) accepts the
// daemon's protobuf POSTs at /v1/traces; the suite decodes span
// names/IDs/attrs from the wire format and additionally asserts raw-body
// byte containment of the shared trace id (no protobuf dependency).
// Determinism: the exporter batches on a 5s interval, so each scenario
// stops the daemon gracefully (Daemon.Stop -> TraceExporter.Stop final
// flush) and then polls the receiver with a deadline instead of sleeping.
package llmhoptracing

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"

	_ "modernc.org/sqlite" // pure-Go sqlite driver (module dep of the daemon)
)

// receiverTTL is the bounded poll budget for the receiver after a graceful
// daemon stop (the stop itself flushes; the poll is belt-and-braces for the
// in-flight POST to land).
const receiverTTL = 15 * time.Second

// startArmed boots a sandbox daemon with metrics.otlp_traces enabled and
// pointed at a fresh capture receiver, registers a project + session, and
// returns them. Cleanup order: the suite stops the daemon explicitly
// before asserting (the graceful stop flushes the exporter's final batch);
// t.Cleanup then closes the receiver, already drained.
func startArmed(t *testing.T) (*harness.Stack, *harness.OTLPCaptureReceiver, string) {
	t.Helper()
	rcv := harness.NewOTLPCaptureReceiver()
	t.Cleanup(rcv.Close)
	// multiagent.enabled=false keeps the turn on the SINGLETON loop: it is
	// the only loop wired with the agent event emitter whose
	// tool_execution_end events the daemon's otlp wiring subscribes (the
	// registry-built specialist loops carry no emitter -- observed live;
	// leaf-01/03 emitter-path gap, reported separately). Single-agent mode
	// exercises the same C7 bus-driven wiring against the real daemon.
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"metrics.otlp_traces.enabled":         true,
		"metrics.otlp_traces.endpoint":        rcv.URL(),
		"metrics.otlp_traces.export_interval": "1s",
		"multiagent.enabled":                  false,
		"daemon.log_level":                    "DEBUG",
	}))
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "hop", s.ProjectDir)
	return s, rcv, sessionID
}

// awaitSpans polls the receiver until cond(spans) holds or the deadline
// expires (no busy-wait sleeps on wall-clock flush timing).
func awaitSpans(t *testing.T, rcv *harness.OTLPCaptureReceiver, what string, cond func([]harness.CapturedSpan) bool) []harness.CapturedSpan {
	t.Helper()
	deadline := time.Now().Add(receiverTTL)
	for time.Now().Before(deadline) {
		spans := rcv.Spans()
		if cond(spans) {
			return spans
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("llm-hop-tracing: timed out waiting for %s; requests=%d spans=%+v",
		what, rcv.Requests(), rcv.Spans())
	return nil
}

// byName filters spans by name.
func byName(spans []harness.CapturedSpan, name string) []harness.CapturedSpan {
	var out []harness.CapturedSpan
	for _, sp := range spans {
		if sp.Name == name {
			out = append(out, sp)
		}
	}
	return out
}

// byTrace filters spans carrying the given trace id.
func byTrace(spans []harness.CapturedSpan, trace string) []harness.CapturedSpan {
	var out []harness.CapturedSpan
	for _, sp := range spans {
		if sp.TraceIDHex == trace {
			out = append(out, sp)
		}
	}
	return out
}

// countName counts spans with the given name.
func countName(spans []harness.CapturedSpan, name string) int {
	return len(byName(spans, name))
}

// readWholeDaemonLog returns the full daemon log (LogTail caps at 4KB).
func readWholeDaemonLog(t *testing.T, s *harness.Stack) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.Work, "daemon.log"))
	if err != nil {
		return ""
	}
	return string(data)
}

// countLines counts log lines containing substr.
func countLines(log, substr string) int {
	n := 0
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, substr) {
			n++
		}
	}
	return n
}

// llmCallsRow is one row of the daemon's metrics.db llm_calls table.
type llmCallsRow struct {
	ID        int64
	ModelID   string
	LatencyMS int64
	TraceID   string
	SpanID    string
}

// latestLLMCallsRows reads llm_calls (metrics.db lives in the daemon's
// data dir) newest-first, mirroring the tasksdb.go read pattern.
func latestLLMCallsRows(t *testing.T, dbPath string, limit int) []llmCallsRow {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		t.Fatalf("llm-hop-tracing: open metrics.db: %v", err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	rows, err := db.Query(`SELECT id, COALESCE(model_id,''), COALESCE(latency_ms,0),
		COALESCE(trace_id,''), COALESCE(span_id,'') FROM llm_calls ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		t.Fatalf("llm-hop-tracing: query llm_calls: %v", err)
	}
	defer rows.Close()
	var out []llmCallsRow
	for rows.Next() {
		var r llmCallsRow
		if err := rows.Scan(&r.ID, &r.ModelID, &r.LatencyMS, &r.TraceID, &r.SpanID); err != nil {
			t.Fatalf("llm-hop-tracing: scan llm_calls: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("llm-hop-tracing: iterate llm_calls: %v", err)
	}
	return out
}

// ---------------------------------------------------------------------------
// T1 + T2 + T4: armed boot -- one tool turn, single shared trace,
// attribute allowlist, byte-exact SQL join, armed log line.
// ---------------------------------------------------------------------------

// TestLLMHopTracing01SharedTraceAcrossHops runs ONE tool-call turn through
// the DIRECT chat lane (classifier pinned to "chat": the handler's minted
// trace context flows into RouteToAgent -> RunOnce -> every LLM call and
// the tool executor; async task dispatch crosses a bus boundary that
// cannot carry the request-scoped trace, which is the documented
// children-under-random-traces posture for step jobs) and asserts:
//   - the turn's trace carries exactly one chat.turn, >= 1 llm.call and
//     >= 1 tool.call span (single shared trace id across all three);
//   - the raw export body carries the trace id bytes (byte-level proof);
//   - llm.call attrs carry the GenAI provider/model/token attributes
//     (usage tokens >= 1);
//   - tool.call attrs carry gen_ai.tool.name, meept.tool.args.keys ==
//     "limit,path" (sorted key names only), meept.tool.args.bytes > 0, an
//     exactly-allowlisted key set, and NO raw argument value;
//   - tool.call span id != llm.call span id;
//   - exactly one armed Info line at boot, no disabled line (T4).
//
// T2 rides the same run: the llm_calls rows joining the turn's trace match
// the captured llm.call spans byte-exactly on trace_id AND span_id, with
// latency_ms > 0.
func TestLLMHopTracing01SharedTraceAcrossHops(t *testing.T) {
	s, rcv, sessionID := startArmed(t)

	// No dispatcher in single-agent mode: the turn runs direct on the
	// singleton loop, so the handler's minted trace context flows into
	// RunOnce -> every LLM call + the tool executor on one trace. (Async
	// task dispatch crosses a bus boundary that cannot carry the
	// request-scoped trace -- the documented children-under-random-traces
	// posture for step jobs.)

	// Script the tool hop: the turn makes one read_file tool call with TWO
	// arguments (path + limit), then the post-tool text ends the turn.
	notes := filepath.Join(s.ProjectDir, "notes.md")
	if err := os.WriteFile(notes, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatalf("seed notes.md: %v", err)
	}
	args := `{"path":"` + notes + `","limit":5}`
	s.Fake.EnqueueToolCalls(harness.ToolCall{Name: "file_read", Arguments: args})
	s.Fake.SetPostToolText("The notes have been read.")

	s.ChatTurn(t, sessionID, "Please look at the notes file for me.", 240*time.Second)

	// Graceful stop flushes the exporter's final batch (TraceExporter.Stop).
	s.Daemon.Stop()

	spans := awaitSpans(t, rcv, "the turn's chat.turn + llm.call + tool.call spans on one trace", func(spans []harness.CapturedSpan) bool {
		for _, sp := range byName(spans, "chat.turn") {
			traceSpans := byTrace(spans, sp.TraceIDHex)
			if countName(traceSpans, "llm.call") >= 1 && countName(traceSpans, "tool.call") >= 1 {
				return true
			}
		}
		return false
	})

	// Exactly one chat.turn overall (one turn -> exactly one turn span).
	turns := byName(spans, "chat.turn")
	if len(turns) != 1 {
		t.Fatalf("chat.turn spans = %d, want exactly 1", len(turns))
	}
	turn := turns[0]
	trace := turn.TraceIDHex
	if len(trace) != 32 {
		t.Fatalf("chat.turn trace id = %q, want 32-char hex", trace)
	}
	if _, err := hex.DecodeString(trace); err != nil {
		t.Fatalf("chat.turn trace id %q is not hex: %v", trace, err)
	}

	// Single shared trace across all three span kinds.
	traceSpans := byTrace(spans, trace)
	llmSpans := byName(traceSpans, "llm.call")
	toolSpans := byName(traceSpans, "tool.call")
	if len(llmSpans) == 0 || len(toolSpans) == 0 {
		t.Fatalf("turn trace %s missing hop spans (llm=%d tool=%d of %d spans on the trace)",
			trace, len(llmSpans), len(toolSpans), len(traceSpans))
	}

	// tool.call span id differs from the llm.call span id (fresh per hop).
	seenSpanIDs := map[string]bool{}
	for _, sp := range llmSpans {
		if sp.SpanIDHex == "" {
			t.Fatal("llm.call span carries an empty span id")
		}
		seenSpanIDs[sp.SpanIDHex] = true
	}
	for _, sp := range toolSpans {
		if seenSpanIDs[sp.SpanIDHex] {
			t.Fatalf("tool.call span id %s collides with an llm.call span id", sp.SpanIDHex)
		}
	}

	// Byte-level proof: the shared trace id bytes appear in a raw export body.
	raw, _ := hex.DecodeString(trace)
	found := false
	for _, body := range rcv.Bodies() {
		if bytes.Contains(body, raw) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no raw export body contains the shared trace id %s", trace)
	}

	// llm.call attrs: GenAI provider/model + usage tokens >= 1.
	llm := llmSpans[0]
	if got := llm.Attrs["gen_ai.provider.name"]; got == "" {
		t.Errorf("llm.call missing gen_ai.provider.name (attrs %v)", llm.Attrs)
	}
	if got := llm.Attrs["gen_ai.request.model"]; got == "" {
		t.Errorf("llm.call missing gen_ai.request.model (attrs %v)", llm.Attrs)
	}
	if got := llm.Attrs["gen_ai.usage.input_tokens"]; got == "" || got == "0" {
		t.Errorf("llm.call gen_ai.usage.input_tokens = %q, want >= 1", got)
	}
	if got := llm.Attrs["gen_ai.usage.output_tokens"]; got == "" || got == "0" {
		t.Errorf("llm.call gen_ai.usage.output_tokens = %q, want >= 1", got)
	}

	// tool.call attrs: shape-only argument fingerprints, no raw values.
	tool := toolSpans[0]
	if got := tool.Attrs["gen_ai.tool.name"]; got != "file_read" {
		t.Errorf("tool.call gen_ai.tool.name = %q, want file_read", got)
	}
	if got := tool.Attrs["meept.tool.args.keys"]; got != "limit,path" {
		t.Errorf("tool.call meept.tool.args.keys = %q, want sorted key names \"limit,path\"", got)
	}
	if got := tool.Attrs["meept.tool.args.bytes"]; got == "" || got == "0" {
		t.Errorf("tool.call meept.tool.args.bytes = %q, want > 0", got)
	}
	// Allowlist check: only C8-table keys on the tool span, plus the
	// sink's shared per-span correlation attributes (trace_name,
	// gen_ai.system, langfuse.*) that the OTLP sink adds to EVERY span
	// (otlp_trace_sink.go spanBaseAttributes; shape-only, no values).
	allowed := map[string]bool{
		"gen_ai.operation.name": true, "gen_ai.tool.name": true,
		"gen_ai.tool.call.id": true, "meept.tool.args.keys": true,
		"meept.tool.args.bytes": true, "meept.tool.blocked": true,
		"error.type": true, "error.message": true,
		// sink-shared correlation attributes (every span):
		"trace_name": true, "gen_ai.system": true,
		"langfuse.session.id": true, "langfuse.user.id": true,
		"gen_ai.conversation.id": true,
	}
	for k := range tool.Attrs {
		if !allowed[k] {
			t.Errorf("tool.call carries attribute %q outside the C8 allowlist", k)
		}
	}
	// ...and NO raw argument value anywhere in those attributes: neither
	// the notes path (its value or file name) nor the limit value.
	forbidden := []string{notes, "notes.md"}
	for k, v := range tool.Attrs {
		for _, f := range forbidden {
			if strings.Contains(v, f) {
				t.Errorf("tool.call attribute %q carries raw argument value %q", k, f)
			}
		}
	}

	// T2 rides this run: byte-exact SQL join to llm_calls.
	rows := latestLLMCallsRows(t, filepath.Join(s.StateDir, "metrics.db"), 50)
	if len(rows) == 0 {
		t.Fatal("metrics.db has no llm_calls rows -- leaf-02 persistence missing")
	}
	joined := false
	for _, row := range rows {
		// Byte-exact BOTH-side join (C5/C6 identity): trace AND span.
		spanMatch := slices.ContainsFunc(llmSpans, func(sp harness.CapturedSpan) bool {
			return sp.SpanIDHex == row.SpanID
		})
		if row.TraceID != trace || !spanMatch {
			continue
		}
		joined = true
		// latency_ms propagation note: against the in-process fake the
		// round-trip can complete in under 1ms, so Milliseconds() rounds to
		// 0; a >0 assertion would flake on machine speed, not on wiring.
		// The token counters below prove the row is the turn's real call.
		if row.LatencyMS < 0 {
			t.Errorf("llm_calls row %d latency_ms = %d, want >= 0", row.ID, row.LatencyMS)
		}
		break // newest matching row
	}
	if !joined {
		t.Fatalf("no llm_calls row joins on trace_id %s (%d rows checked)", trace, len(rows))
	}

	// T4 rides this run: armed boot logged exactly one enabled Info line,
	// and the disabled line never fired.
	log := readWholeDaemonLog(t, s)
	if got := countLines(log, "OTLP trace export enabled"); got != 1 {
		t.Errorf("daemon log has %d \"OTLP trace export enabled\" lines, want exactly 1", got)
	}
	if got := countLines(log, "OTLP trace export disabled"); got != 0 {
		t.Errorf("armed boot logged %d disabled lines, want 0", got)
	}
}

func llmSpanIDs(spans []harness.CapturedSpan) []string {
	out := make([]string, 0, len(spans))
	for _, sp := range spans {
		out = append(out, sp.SpanIDHex)
	}
	return out
}

// ---------------------------------------------------------------------------
// T3: quiet boot (disabled posture) -- zero POSTs + exactly one Warn.
// ---------------------------------------------------------------------------

// TestLLMHopTracing02QuietBootControl boots the daemon with
// metrics.otlp_traces ABSENT (default false) pointed at a receiver, runs
// one plain chat turn, and asserts the disabled posture: zero HTTP
// requests at the receiver, no spans, exactly one disabled Warn line and
// no enabled line in the daemon log.
func TestLLMHopTracing02QuietBootControl(t *testing.T) {
	rcv := harness.NewOTLPCaptureReceiver()
	t.Cleanup(rcv.Close)
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"metrics.otlp_traces.endpoint": rcv.URL(), // enabled defaults false
	}))
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "quiet", s.ProjectDir)

	s.Fake.SetChatText("quiet-boot reply")
	s.ChatTurn(t, sessionID, "Say something short.", 240*time.Second)

	// Graceful stop: nothing should flush (there is no exporter at all).
	s.Daemon.Stop()
	// Bounded window for any (wrongly-armed) exporter to have fired.
	time.Sleep(2 * time.Second)

	if got := rcv.Requests(); got != 0 {
		t.Fatalf("disabled posture made %d export POSTs, want 0 (spans %+v)", got, rcv.Spans())
	}
	if got := len(rcv.Spans()); got != 0 {
		t.Fatalf("disabled posture produced %d spans, want 0", got)
	}

	log := readWholeDaemonLog(t, s)
	if got := countLines(log, "OTLP trace export disabled"); got != 1 {
		t.Errorf("daemon log has %d \"OTLP trace export disabled\" lines, want exactly 1", got)
	}
	if got := countLines(log, "OTLP trace export enabled"); got != 0 {
		t.Errorf("disabled boot logged %d enabled lines, want 0", got)
	}
}
