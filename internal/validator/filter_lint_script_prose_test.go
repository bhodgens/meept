package validator

import (
	"context"
	"testing"
)

// Run 33 regression: a chat reply's narration fenced as ```js must not fail
// the whole turn through node --check ("snippet_0.js:1 ... Unexpected
// identifier"). Prose blocks are skipped; genuine code still lints.
func TestJSLintFilterSkipsProseFencedAsJS(t *testing.T) {
	f := NewJSLintFilter("node", "")
	res := f.Process(context.Background(), nil,
		"the file is created at the requested path\n\n```js\nthe file is at hello.txt in the project folder\n```\n")
	if res.Outcome != FilterPass {
		t.Fatalf("Outcome = %v, want pass (prose in a js fence is skipped): %s", res.Outcome, res.Reason)
	}
}

func TestJSLintFilterAdvisoryOnBadJSCode(t *testing.T) {
	f := NewJSLintFilter("node", "")
	res := f.Process(context.Background(), nil,
		"```js\nconst x = ;\n```\n")
	if res.Outcome != FilterAdvisory {
		t.Fatalf("Outcome = %v, want advisory (real syntax errors logged, not turn-fatal)", res.Outcome)
	}
	if res.Reason == "" {
		t.Fatal("advisory result carries no diagnostic Reason")
	}
}

func TestLooksLikeProse(t *testing.T) {
	cases := []struct {
		name string
		code string
		want bool
	}{
		{"narration sentence", "the file is at hello.txt in the project folder", true},
		{"const declaration", "const path = require('path');", false},
		{"function", "function beep() {\n  return 1;\n}", false},
		{"import", "import fs from 'fs';", false},
		{"short line", "hello", false},
		{"identifier start", "myVar value here now", false},
		{"capitalized code keyword", "Const x = 1", false},
		{"capitalized narration", "The file is at hello.txt in the folder", true},
	}
	for _, tc := range cases {
		if got := looksLikeProse(tc.code); got != tc.want {
			t.Errorf("%s: looksLikeProse = %v, want %v", tc.name, got, tc.want)
		}
	}
}
