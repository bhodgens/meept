package llm

import (
	"encoding/json"
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
