package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestCompressMapResult_DeterministicByteIdentical: a map with 8 mixed-size
// keys under a TIGHT budget (forcing clipping) must produce byte-identical
// JSON across 100 runs. Today's implementation iterates the Go map in
// randomized order: the first oversized string clips and every subsequent
// key in iteration order is dropped wholesale, so which keys survive flips
// run to run and the bytes change.
//
// Note: under a GENEROUS budget this check is vacuous (every key survives
// and encoding/json sorts map keys on marshal), which is why the budget
// here is tight — see the primary-selection and metadata tests for the
// semantic contract the determinism protects.
func TestCompressMapResult_DeterministicByteIdentical(t *testing.T) {
	m := map[string]any{}
	for i := 0; i < 8; i++ {
		m[fmt.Sprintf("key_%d", i)] = strings.Repeat("x", 50*(i+1))
	}
	const maxChars = 1200 // content sums to ~1800 chars: clipping is forced

	var first string
	for run := 0; run < 100; run++ {
		out := compressMapResult(m, maxChars)
		data, err := json.Marshal(out)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if run == 0 {
			first = string(data)
			continue
		}
		if string(data) != first {
			t.Fatalf("run %d produced different bytes:\nfirst: %s\ngot:   %s", run, first, data)
		}
	}
}

// TestCompressMapResult_TightBudgetKeepsAllKeys: under a tight budget the
// deterministic rewrite must still emit EVERY input key (primary truncated
// to the remaining budget, non-primary copied whole) — today's randomized
// wholesale-drop cannot guarantee this. encoding/json always emits map keys
// in sorted order, so key PRESENCE + content identity (not emission order)
// is the observable surface this asserts.
func TestCompressMapResult_TightBudgetKeepsAllKeys(t *testing.T) {
	m := map[string]any{
		"content": strings.Repeat("c", 1000),
		"alpha":   "short-a",
		"bravo":   "short-b",
		"charlie": strings.Repeat("h", 300),
	}
	out := compressMapResult(m, 1200)

	if out["_truncated"] != true {
		t.Errorf("_truncated missing or false on clipped primary")
	}
	for _, key := range []string{"content", "alpha", "bravo", "charlie"} {
		if _, ok := out[key]; !ok {
			t.Errorf("key %q dropped from compressed output: %v", key, out)
		}
	}
	if got := out["alpha"].(string); got != "short-a" {
		t.Errorf("alpha = %q, want whole", got)
	}
	if got := out["charlie"].(string); len(got) != 300 {
		t.Errorf("charlie not whole (len=%d, want 300)", len(got))
	}
	if len(out["content"].(string)) >= 1000 {
		t.Errorf("content not truncated despite tight budget")
	}
}

// TestCompressMapResult_PrimarySelection verifies primary-key precedence:
// content > output > result > longest-string-value (tie -> lexicographically
// first key).
func TestCompressMapResult_PrimarySelection(t *testing.T) {
	t.Run("content preferred over output and result", func(t *testing.T) {
		m := map[string]any{
			"result":  strings.Repeat("r", 50),
			"output":  strings.Repeat("o", 50),
			"content": strings.Repeat("c", 50),
		}
		out := compressMapResult(m, 60) // tight budget so only primary clips
		// content must be the truncated-with-marker one; output/result whole.
		content := out["content"].(string)
		if !strings.Contains(content, "truncated") {
			t.Errorf("primary (content) not truncated under tight budget: %q", content)
		}
		if got := out["output"].(string); got != strings.Repeat("o", 50) {
			t.Errorf("output metadata key not preserved whole: %q", got)
		}
		if got := out["result"].(string); got != strings.Repeat("r", 50) {
			t.Errorf("result metadata key not preserved whole: %q", got)
		}
	})

	t.Run("longest string wins when no well-known keys", func(t *testing.T) {
		m := map[string]any{
			"short":  "abc",
			"longer": strings.Repeat("L", 500),
			"n":      7.0,
		}
		out := compressMapResult(m, 60)
		longer := out["longer"].(string)
		if !strings.Contains(longer, "truncated") {
			t.Errorf("longest string not treated as primary (not truncated): %q", longer)
		}
		if got := out["short"].(string); got != "abc" {
			t.Errorf("short key not preserved whole: %q", got)
		}
	})

	t.Run("tie broken by lexicographically first key", func(t *testing.T) {
		// Same-length strings, budget smaller than their sum so exactly the
		// primary truncates.
		long := strings.Repeat("z", 100)
		m := map[string]any{
			"zzz_second": long,
			"aaa_first":  long,
		}
		out := compressMapResult(m, 120)
		if got := out["aaa_first"].(string); !strings.Contains(got, "truncated") {
			t.Errorf("aaa_first should be primary (lex-first on tie) and truncate, got: %q", got)
		}
		if got := out["zzz_second"].(string); got != long {
			t.Errorf("zzz_second not preserved whole: %q", got)
		}
	})
}

// TestCompressMapResult_MetadataIntegrity: a transcript-shaped map (large
// content + path/offset/total_chars metadata) under a tight budget must keep
// the metadata FULL while truncating only the primary.
func TestCompressMapResult_MetadataIntegrity(t *testing.T) {
	path := "/tmp/transcripts/full.txt"
	m := map[string]any{
		"path":        path,
		"offset":      42.0,
		"total_chars": 51234.0,
		"content":     strings.Repeat("t", 50000),
	}
	out := compressMapResult(m, 2000)

	if got := out["path"].(string); got != path {
		t.Errorf("path = %q, want full %q", got, path)
	}
	if got := out["offset"].(float64); got != 42.0 {
		t.Errorf("offset = %v, want 42", got)
	}
	if got := out["total_chars"].(float64); got != 51234.0 {
		t.Errorf("total_chars = %v, want 51234", got)
	}
	content := out["content"].(string)
	if len(content) >= 50000 {
		t.Errorf("content not truncated (len=%d)", len(content))
	}
	if !strings.Contains(content, "truncated") {
		t.Errorf("content truncation missing marker: %q", content[:min(len(content), 100)])
	}
	if out["_truncated"] != true {
		t.Errorf("_truncated missing or false")
	}
}

// TestCompressMapResult_MetadataOverflow: non-primary keys alone exceeding
// maxChars must ALL survive whole with _truncated=true — metadata integrity
// outranks the budget.
func TestCompressMapResult_MetadataOverflow(t *testing.T) {
	m := map[string]any{
		"content": "small",
	}
	// 30 non-primary keys x 100 chars = 3000 chars > maxChars 2000.
	for i := 0; i < 30; i++ {
		m[fmt.Sprintf("meta_%02d", i)] = strings.Repeat("m", 100)
	}
	out := compressMapResult(m, 2000)

	if out["_truncated"] != true {
		t.Errorf("_truncated missing or false on metadata overflow")
	}
	// Metadata overflow drives the remaining budget negative, so the primary
	// clips to the marker per the contract (remaining < len).
	if got := out["content"].(string); !strings.Contains(got, "truncated") {
		t.Errorf("content = %q, want clipped-to-marker on overflow", got)
	}
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("meta_%02d", i)
		got, ok := out[key].(string)
		if !ok {
			t.Fatalf("metadata key %s missing from output", key)
		}
		if len(got) != 100 {
			t.Errorf("metadata key %s not whole (len=%d, want 100)", key, len(got))
		}
	}
}

// TestCompressMapResult_NoStringsAndEmpty: a map with no string values (and
// the empty map) takes the copy-all path with no _truncated flag.
func TestCompressMapResult_NoStringsAndEmpty(t *testing.T) {
	t.Run("no string values", func(t *testing.T) {
		m := map[string]any{"n": 5.0, "flag": true, "list": []any{1.0, 2.0}}
		out := compressMapResult(m, 10)
		if out["_truncated"] != nil {
			t.Errorf("_truncated set on no-string map: %v", out)
		}
		if got := out["n"].(float64); got != 5.0 {
			t.Errorf("n = %v, want 5", got)
		}
		if got := out["flag"].(bool); !got {
			t.Errorf("flag = %v, want true", got)
		}
	})

	t.Run("empty map", func(t *testing.T) {
		out := compressMapResult(map[string]any{}, 10)
		if len(out) != 0 {
			t.Errorf("empty map produced output %v", out)
		}
	})
}

// TestCompressMapResult_PrimaryFits: when the primary fits within the
// remaining budget there is no _truncated flag and the content is whole.
func TestCompressMapResult_PrimaryFits(t *testing.T) {
	m := map[string]any{
		"path":    "/tmp/x.txt",
		"content": strings.Repeat("k", 500),
	}
	out := compressMapResult(m, 4000)
	if out["_truncated"] != nil {
		t.Errorf("_truncated set though everything fits: %v", out)
	}
	if got := out["content"].(string); len(got) != 500 {
		t.Errorf("content len = %d, want 500 (whole)", len(got))
	}
}
