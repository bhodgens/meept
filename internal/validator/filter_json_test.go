package validator

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

func TestJSONFormatFilter_Applies(t *testing.T) {
	f := NewJSONFormatFilter()
	tests := []struct {
		name   string
		step   *task.TaskStep
		expect bool
	}{
		{"json tool hint", &task.TaskStep{ToolHint: "json"}, true},
		{"http tool hint", &task.TaskStep{ToolHint: "http_request"}, true},
		{"case insensitive", &task.TaskStep{ToolHint: "Curl"}, true},
		{"prose tool hint", &task.TaskStep{ToolHint: "write_file"}, false},
		{"empty tool hint", &task.TaskStep{}, false},
		{"nil step", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := f.Applies(tc.step); got != tc.expect {
				t.Fatalf("Applies = %v, want %v", got, tc.expect)
			}
		})
	}
}

func TestJSONFormatFilter_Process(t *testing.T) {
	f := NewJSONFormatFilter()
	ctx := context.Background()
	step := &task.TaskStep{ToolHint: "json"}

	tests := []struct {
		name       string
		output     string
		want       FilterOutcome
		wantSubstr string // Reason substring when want == FilterFail
	}{
		{"empty output passes", "", FilterPass, ""},
		{"whitespace passes", "   \n\t ", FilterPass, ""},
		{"invalid json fails with offset", `{"a":`, FilterFail, "invalid JSON at offset"},
		{"trailing garbage fails", `{"a":1} junk`, FilterFail, "invalid JSON at offset"},
		{"two values fail", "1 2", FilterFail, "invalid JSON at offset"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := f.Process(ctx, step, tc.output)
			if !res.Valid() {
				t.Fatalf("result invalid: %+v", res)
			}
			if res.Outcome != tc.want {
				t.Fatalf("Outcome = %v, want %v (reason=%q output=%q)", res.Outcome, tc.want, res.Reason, res.Output)
			}
			if tc.wantSubstr != "" && !strings.Contains(res.Reason, tc.wantSubstr) {
				t.Fatalf("Reason %q missing substring %q", res.Reason, tc.wantSubstr)
			}
		})
	}
}

func TestJSONFormatFilter_CanonicalRewrite(t *testing.T) {
	f := NewJSONFormatFilter()
	ctx := context.Background()
	step := &task.TaskStep{ToolHint: "json"}

	// Pretty-printed valid JSON with UNSORTED keys, 4-space indent.
	input := "{\n    \"zebra\": 1,\n    \"apple\": [3, 1, 2],\n    \"mango\": {\"y\": true, \"n\": null}\n}"
	res := f.Process(ctx, step, input)
	if res.Outcome != FilterRewrite {
		t.Fatalf("Outcome = %v, want rewrite (reason=%q)", res.Outcome, res.Reason)
	}

	want := "{\n  \"apple\": [\n    3,\n    1,\n    2\n  ],\n  \"mango\": {\n    \"n\": null,\n    \"y\": true\n  },\n  \"zebra\": 1\n}\n"
	if res.Output != want {
		t.Fatalf("canonical form mismatch:\n got: %q\nwant: %q", res.Output, want)
	}

	// Round-trip: compact JSON of the same value rewrites to the SAME
	// canonical bytes (sorted keys, 2-space indent, trailing newline).
	compact := `{"zebra":1,"apple":[3,1,2],"mango":{"y":true,"n":null}}`
	if res2 := f.Process(ctx, step, compact); res2.Outcome != FilterRewrite || res2.Output != want {
		t.Fatalf("compact form did not canonicalize identically: %+v", res2)
	}

	// Numbers keep precision (json.Number round-trip).
	big := `{"n":12345678901234567890}`
	res3 := f.Process(ctx, step, big)
	if res3.Outcome != FilterRewrite || !strings.Contains(res3.Output, "12345678901234567890") {
		t.Fatalf("big integer altered: %+v", res3)
	}
}

func TestJSONFormatFilter_Idempotent(t *testing.T) {
	f := NewJSONFormatFilter()
	ctx := context.Background()
	step := &task.TaskStep{ToolHint: "json"}

	input := `{"b":2,"a":{"d":4,"c":[1,2]},"e":true}`
	first := f.Process(ctx, step, input)
	if first.Outcome != FilterRewrite {
		t.Fatalf("first pass Outcome = %v, want rewrite", first.Outcome)
	}
	// Feeding the filter's own output back MUST pass (byte-identical to
	// canonical form) - the idempotency proof.
	second := f.Process(ctx, step, first.Output)
	if second.Outcome != FilterPass {
		t.Fatalf("second pass Outcome = %v, want pass (reason=%q)", second.Outcome, second.Reason)
	}
	if second.Output != "" {
		t.Fatalf("second pass must not carry output, got %q", second.Output)
	}
}
