package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseLFMToolCalls_SingleCall(t *testing.T) {
	in := `<|tool_call_start|>[file_write(path="hello.txt", content="hello")]<|tool_call_end|>`
	content, calls := parseLFMToolCalls(in)
	if content != "" {
		t.Errorf("content = %q, want empty", content)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Function.Name != "file_write" {
		t.Errorf("name = %q, want file_write", calls[0].Function.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("args not valid JSON: %v", err)
	}
	if args["path"] != "hello.txt" || args["content"] != "hello" {
		t.Errorf("args = %v, want path/content", args)
	}
}

func TestParseLFMToolCalls_NoMarkers(t *testing.T) {
	in := "just plain text with no markers"
	content, calls := parseLFMToolCalls(in)
	if content != in || calls != nil {
		t.Errorf("plain content mutated: %q, %v", content, calls)
	}
}

func TestParseLFMToolCalls_MultipleAndProse(t *testing.T) {
	in := "Let me do two things.\n<|tool_call_start|>[file_read(path=\"/tmp/a\")]<|tool_call_end|>\n<|tool_call_start|>[shell_execute(command=\"ls -la\", timeout=30)]<|tool_call_end|>"
	content, calls := parseLFMToolCalls(in)
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	if calls[1].Function.Name != "shell_execute" {
		t.Errorf("second call name = %q", calls[1].Function.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[1].Function.Arguments), &args); err != nil {
		t.Fatalf("args invalid: %v", err)
	}
	if args["timeout"] != float64(30) {
		t.Errorf("timeout = %v, want 30", args["timeout"])
	}
	if content != "Let me do two things." {
		t.Errorf("prose = %q, want markers stripped", content)
	}
}

func TestParseLFMToolCalls_CommaInsideString(t *testing.T) {
	in := `<|tool_call_start|>[file_write(path="a,b.txt", content="x=1, y=2")]<|tool_call_end|>`
	_, calls := parseLFMToolCalls(in)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("args invalid: %v", err)
	}
	if args["path"] != "a,b.txt" || args["content"] != "x=1, y=2" {
		t.Errorf("quoted commas mangled: %v", args)
	}
}

func TestParseLFMToolCalls_BooleansAndNumbers(t *testing.T) {
	in := `<|tool_call_start|>[shell_execute(command="ls", direct=true, timeout=30)]<|tool_call_end|>`
	_, calls := parseLFMToolCalls(in)
	if len(calls) != 1 {
		t.Fatalf("got %d calls", len(calls))
	}
	var args map[string]any
	_ = json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
	if args["direct"] != true || args["timeout"] != float64(30) {
		t.Errorf("typed args wrong: %v", args)
	}
}

// EXACT shape recovered from e2e run 7 (2026-09-10, run RW5VvO tasks.db):
// the model emitted the call as a fenced JSON object with the SHORT args
// key, in plain message content. Pinned verbatim — do not "normalize" the
// fence text; it is the model's observed emission.
func TestParseLFMFenceCalls_Run7JSONFence(t *testing.T) {
	in := "I'll create the file now.\n```json\n{\"name\":\"file_write\",\"args\":{\"path\":\"hello.txt\",\"content\":\"hello\"}}\n```\nDone!"
	content, calls := parseLFMToolCalls(in)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1; content=%q", len(calls), content)
	}
	if calls[0].Function.Name != "file_write" {
		t.Errorf("name = %q, want file_write", calls[0].Function.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("args invalid: %v", err)
	}
	if args["path"] != "hello.txt" || args["content"] != "hello" {
		t.Errorf("args = %v, want path/content", args)
	}
	if strings.Contains(content, "file_write") || strings.Contains(content, "```") {
		t.Errorf("fence not stripped from content: %q", content)
	}
	if !strings.Contains(content, "I'll create the file now.") ||
		!strings.Contains(content, "Done!") {
		t.Errorf("surrounding prose lost: %q", content)
	}
}

// arguments-as-object long key, plus no language tag on the fence.
func TestParseLFMFenceCalls_ArgumentsKeyNoTag(t *testing.T) {
	in := "```\n{\"name\":\"file_read\",\"arguments\":{\"path\":\"/tmp/a\"}}\n```"
	content, calls := parseLFMToolCalls(in)
	if len(calls) != 1 || calls[0].Function.Name != "file_read" {
		t.Fatalf("got %v, want 1 file_read call; content=%q", calls, content)
	}
	if content != "" {
		t.Errorf("content = %q, want empty", content)
	}
}

// OpenAI function-call wire shape with stringified arguments.
func TestParseLFMFenceCalls_OpenAIShape(t *testing.T) {
	in := "```json\n{\"function\":{\"name\":\"file_write\",\"arguments\":\"{\\\"path\\\":\\\"x.txt\\\",\\\"content\\\":\\\"y\\\"}\"}}\n```"
	_, calls := parseLFMToolCalls(in)
	if len(calls) != 1 || calls[0].Function.Name != "file_write" {
		t.Fatalf("got %v, want 1 file_write call", calls)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("args invalid: %v", err)
	}
	if args["path"] != "x.txt" || args["content"] != "y" {
		t.Errorf("args = %v", args)
	}
}

// Plain fenced code (no tool-call shape) must survive untouched.
func TestParseLFMFenceCalls_PlainCodeFenceUntouched(t *testing.T) {
	in := "use this:\n```json\n{\"status\":\"ok\",\"data\":[1,2,3]}\n```\nthanks"
	content, calls := parseLFMToolCalls(in)
	if len(calls) != 0 {
		t.Fatalf("minted calls from non-call fence: %v", calls)
	}
	if content != in {
		t.Errorf("plain fence mutated:\n got %q\nwant %q", content, in)
	}
}

// Shell/code fences are never touched.
func TestParseLFMFenceCalls_ShellFenceUntouched(t *testing.T) {
	in := "run:\n```bash\nls -la | grep hello\n```"
	content, calls := parseLFMToolCalls(in)
	if len(calls) != 0 || content != in {
		t.Fatalf("calls=%v content=%q, want untouched", calls, content)
	}
}

// A JSON ARRAY of direct-shape call objects inside one fence must NOT be
// mined (the object parser only accepts a single object; an array body
// fails Unmarshal into map and the fence survives — correctness over
// recoverability for this shape, which has not been observed in the wild).
func TestParseLFMFenceCalls_ArrayBodyUntouched(t *testing.T) {
	in := "```json\n[{\"name\":\"file_write\",\"args\":{\"path\":\"a\"}}]\n```"
	content, calls := parseLFMToolCalls(in)
	if len(calls) != 0 || content != in {
		t.Fatalf("calls=%v, want untouched array body", calls)
	}
}

// Two identical fences in one reply must mint DISTINCT IDs.
func TestParseLFMFenceCalls_DuplicateBodiesDistinctIDs(t *testing.T) {
	in := "```json\n{\"name\":\"file_write\",\"args\":{\"path\":\"a.txt\",\"content\":\"x\"}}\n```\nand\n```json\n{\"name\":\"file_write\",\"args\":{\"path\":\"a.txt\",\"content\":\"x\"}}\n```"
	_, calls := parseLFMToolCalls(in)
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	if calls[0].ID == calls[1].ID {
		t.Errorf("duplicate IDs %q for identical fence bodies", calls[0].ID)
	}
}

// Fence recovery composes with the marker and XML halves.
func TestParseLFMToolCalls_AllThreeShapes(t *testing.T) {
	in := `<|tool_call_start|>[file_read(path="/a")]<|tool_call_end|>` +
		"<function_calls>[{\"id\":\"x1\",\"name\":\"list_directory\",\"arguments\":{}}]</function_calls>" +
		"```json\n{\"name\":\"file_write\",\"args\":{\"path\":\"b\",\"content\":\"c\"}}\n```"
	_, calls := parseLFMToolCalls(in)
	if len(calls) != 3 {
		t.Fatalf("got %d calls, want 3: %+v", len(calls), calls)
	}
	want := []string{"file_read", "list_directory", "file_write"}
	for i, w := range want {
		if calls[i].Function.Name != w {
			t.Errorf("call %d = %q, want %q", i, calls[i].Function.Name, w)
		}
	}
}
