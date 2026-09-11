package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Pins for e2e run 3 T1 (2026-09-10): the coder called
//
//	file_write {"content":"hello","direct":"True","path":"hello.txt"}
//
// with direct as a STRING ("True", capital T) — a common small-model
// tool-argument mistake — and the string failed the boolean type
// assertion, so the write was STAGED into the in-memory pending-changes
// registry instead of hitting disk. The claim ("Created file hello.txt")
// was recorded, the step approved, and the harness found no file.
//
// executeWrite must tolerate the quoted-boolean shape: "true"/"True" both
// mean direct. It must NEVER treat an unknown string as true.
func TestExecuteWrite_DirectQuotedBoolStillWrites(t *testing.T) {
	ctx := context.Background()
	tool := NewWriteFileTool(nil) // nil checker: allowed in tests

	// We can't easily call executeWrite without hitting disk resolution
	// guards, so test the coercion helper contract directly via JSON
	// round-trip of the exact run-3 arguments.
	var args map[string]any
	raw := `{"content":"hello","direct":"True","path":"hello.txt"}`
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatal(err)
	}

	if coerceToolBool(args["direct"]) != true {
		t.Fatalf("coerceToolBool(%#v) = false; string %q must coerce to true", args["direct"], args["direct"])
	}
	if coerceToolBool(true) != true {
		t.Fatal("coerceToolBool(true) must be true")
	}
	if coerceToolBool("false") != false {
		t.Fatal("coerceToolBool(\"false\") must be false")
	}
	if coerceToolBool("banana") != false {
		t.Fatal("coerceToolBool(unknown string) must be false")
	}
	if coerceToolBool(nil) != false {
		t.Fatal("coerceToolBool(nil) must be false")
	}
	if coerceToolBool(1.0) != true {
		t.Fatal("coerceToolBool(1.0) must be true (JSON number-as-bool tolerance)")
	}

	_ = ctx
	_ = tool
	_ = strings.TrimSpace
}
