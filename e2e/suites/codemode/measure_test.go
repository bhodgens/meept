//go:build e2e

// Token-savings measurement case (branch 02 leaf 03, T2): a deterministic,
// no-LLM proof of the nested-call context-savings property.
//
// A fixed script calls tools.file_read five times inside ONE codemode run;
// the same five reads as direct executor tool calls produce FIVE tool-result
// entries in the terminal outbound LLM request. The codemode path must
// produce exactly ONE tool-result entry (the C3 Result) — the nested
// results never enter LLM context (pi parity; the savings dimension of
// codemode-05).
//
// Both legs run against the REAL daemon + fake LLM (hermetic: fixed seeded
// files, fixed script, no model variance). Byte counts are measured from
// the recorded outbound request payloads (FakeLLM.Requests): in the
// terminal executor request, the serialized bytes of every role=tool
// message — exactly the tool-result context the LLM would carry.
//
// Method: "terminal-request tool-message bytes" — for the LAST recorded
// executor request (the one carrying the tools array), len(json.Marshal(m))
// over every role=tool message, summed, plus the entry count.
package codemode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// fiveReadsScript renders the fixed nested-call script: five file_read
// calls through the sandbox tool bridge, each targeting one of the five
// seeded files. The projDir marker is substituted by the test with the
// live project dir (absolute paths — the step-job path does not inject a
// session workdir, mirroring the tools-filesystem suite).
func fiveReadsScript(projDir string) string {
	var b strings.Builder
	b.WriteString("(async () => {\n")
	for i := 1; i <= 5; i++ {
		b.WriteString(fmt.Sprintf(
			"\tawait tools.file_read('{\"path\":\"%s/%s\"}');\n",
			projDir, "file"+strconv.Itoa(i)+".txt"))
	}
	b.WriteString("\treturn 'five-reads-done'; })()")
	return b.String()
}

// seedFiveFiles writes the five fixed files into dir and returns their
// paths in read order.
func seedFiveFiles(t *testing.T, dir string) []string {
	t.Helper()
	paths := make([]string, 0, 5)
	for i := 1; i <= 5; i++ {
		p := filepath.Join(dir, "file"+strconv.Itoa(i)+".txt")
		body := "line one of file" + strconv.Itoa(i) + "\nline two of file" + strconv.Itoa(i) + "\n"
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("seed file %s: %v", p, err)
		}
		paths = append(paths, p)
	}
	return paths
}

// terminalToolBytes measures, in the LAST recorded executor request (the
// terminal payload carrying the tools array), the serialized byte size of
// every role=tool message and the count of such messages — the tool-result
// context cost the LLM would pay.
func terminalToolBytes(t *testing.T, f *harness.FakeLLM) (total int, count int) {
	t.Helper()
	var last []map[string]any
	for _, body := range f.Requests() {
		if _, ok := body["tools"].([]any); !ok {
			continue
		}
		msgs, _ := body["messages"].([]any)
		var m []map[string]any
		for _, raw := range msgs {
			if msg, ok := raw.(map[string]any); ok {
				m = append(m, msg)
			}
		}
		last = m
	}
	if last == nil {
		t.Fatalf("no executor request (tools array) recorded in the fake LLM traffic")
	}
	for _, m := range last {
		if role, _ := m["role"].(string); role != "tool" {
			continue
		}
		count++
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal terminal tool message: %v", err)
		}
		total += len(b)
	}
	return total, count
}

// nestedLeg runs the codemode path: one turn, one codemode tool call whose
// script performs the five file_read calls inside the sandbox. The
// terminal request must hold exactly ONE tool-result entry (the C3
// Result).
func nestedLeg(t *testing.T, s *harness.Stack) (int, int) {
	t.Helper()
	script := fiveReadsScript(s.ProjectDir)
	sessionID := s.CreateSession(t, "cm-measure", s.ProjectDir)
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "codemode",
		Arguments: `{"script":` + jsonQuote(script) + `, "timeout_s": 30}`,
	})
	s.ChatTurn(t, sessionID, "Create a codemode nested read named cm-measure", 120*time.Second)

	cmBytes, cmCount := terminalToolBytes(t, s.Fake)
	msgs := terminalRequestMessages(t, s.Fake)
	tools := toolMessages(msgs)
	if len(tools) != 1 {
		t.Fatalf("codemode path: terminal request carries %d tool messages, want exactly 1", len(tools))
	}
	if content, _ := tools[0]["content"].(string); !isC3Object(content) {
		t.Fatalf("codemode tool result is not a C3 Result object: %s", content)
	}
	// Sanity: the run reported success.
	c3, ok := c3Result(toolResultTexts(s.Fake))
	if !ok {
		t.Fatalf("no C3 Result JSON in the recorded tool results")
	}
	if !c3.OK {
		t.Fatalf("nested five-read script failed: %+v", c3)
	}
	return cmBytes, cmCount
}

// directLeg runs the same five file_read calls as direct executor tool
// calls in ONE turn (five enqueued calls, served across the executor
// round-trips). The terminal request must hold FIVE tool-result entries.
func directLeg(t *testing.T, s *harness.Stack, paths []string) (int, int) {
	t.Helper()
	sessionID := s.CreateSession(t, "cm-direct", s.ProjectDir)
	for _, p := range paths {
		s.Fake.EnqueueToolCalls(harness.ToolCall{
			Name:      "file_read",
			Arguments: `{"path":` + jsonQuote(p) + `}`,
		})
	}
	s.ChatTurn(t, sessionID, "Create a direct read summary of the five files in the project", 120*time.Second)
	return terminalToolBytes(t, s.Fake)
}

// TestNestedCallContextSavings proves the 1-vs-5 tool-result property and
// records the byte-savings table (the completion-report evidence for the
// master Integration Test Plan).
func TestNestedCallContextSavings(t *testing.T) {
	s := startCodemodeStack(t, true)
	paths := seedFiveFiles(t, s.ProjectDir)

	cmBytes, cmCount := nestedLeg(t, s)
	drBytes, drCount := directLeg(t, s, paths)

	if cmCount != 1 {
		t.Fatalf("codemode path: %d tool-result entries in the terminal LLM request, want exactly 1", cmCount)
	}
	if drCount != 5 {
		t.Fatalf("direct path: %d tool-result entries in the terminal LLM request, want 5", drCount)
	}

	// Measurement table (method: terminal-request tool-message bytes).
	savings := drBytes - cmBytes
	fmt.Println("=== codemode nested-call context savings (leaf 03 T2, method: terminal-request tool-message bytes) ===")
	fmt.Printf("codemode path:  tool-result entries = %d ; tool-message bytes = %d\n", cmCount, cmBytes)
	fmt.Printf("direct path:    tool-result entries = %d ; tool-message bytes = %d\n", drCount, drBytes)
	fmt.Printf("savings:        %d bytes (%.1f%% of direct) fewer tool-result context bytes via codemode\n",
		savings, 100*float64(savings)/float64(drBytes))
}
