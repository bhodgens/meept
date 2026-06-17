package builtin

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidatePatch_ValidOps(t *testing.T) {
	tests := []struct {
		name  string
		edits []map[string]any
	}{
		{
			name: "replace with legacy anchor",
			edits: []map[string]any{
				{"op": "replace", "anchor": "10:ab", "content": "new line"},
			},
		},
		{
			name: "replace with tagged anchor",
			edits: []map[string]any{
				{"op": "replace", "anchor": "10:a1b2:ab", "content": "new line"},
			},
		},
		{
			name: "insert_after",
			edits: []map[string]any{
				{"op": "insert_after", "anchor": "5:cd", "content": "added line"},
			},
		},
		{
			name: "insert_before",
			edits: []map[string]any{
				{"op": "insert_before", "anchor": "1:ef", "content": "preamble"},
			},
		},
		{
			name: "delete with legacy anchor",
			edits: []map[string]any{
				{"op": "delete", "anchor": "3:gh"},
			},
		},
		{
			name: "delete with tagged anchor",
			edits: []map[string]any{
				{"op": "delete", "anchor": "7:dead:gh"},
			},
		},
		{
			name: "replace_block",
			edits: []map[string]any{
				{"op": "replace_block", "anchor": "5:ab", "content": "new block"},
			},
		},
		{
			name: "delete_block",
			edits: []map[string]any{
				{"op": "delete_block", "anchor": "10:cd"},
			},
		},
		{
			name: "BOF anchor",
			edits: []map[string]any{
				{"op": "insert_after", "anchor": "BOF", "content": "first line"},
			},
		},
		{
			name: "EOF anchor",
			edits: []map[string]any{
				{"op": "insert_before", "anchor": "EOF", "content": "last line"},
			},
		},
		{
			name: "range replace with end_anchor",
			edits: []map[string]any{
				{"op": "replace", "anchor": "5:ab", "end_anchor": "10:cd", "content": "replacement"},
			},
		},
		{
			name: "range delete with end_anchor",
			edits: []map[string]any{
				{"op": "delete", "anchor": "2:ef", "end_anchor": "8:gh"},
			},
		},
		{
			name: "multiple valid edits",
			edits: []map[string]any{
				{"op": "replace", "anchor": "1:ab", "content": "first"},
				{"op": "insert_after", "anchor": "5:cd", "content": "middle"},
				{"op": "delete", "anchor": "10:ef"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidatePatch(tt.edits)
			if errs != nil {
				t.Errorf("expected no errors, got: %v", errs)
			}
		})
	}
}

func TestValidatePatch_InvalidOp(t *testing.T) {
	edits := []map[string]any{
		{"op": "snip", "anchor": "1:ab"},
	}
	errs := ValidatePatch(edits)
	if len(errs) == 0 {
		t.Fatal("expected errors for invalid op")
	}
	found := false
	for _, e := range errs {
		if e.Field == "op" && strings.Contains(e.Message, "'snip'") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected op error mentioning 'snip', got: %v", errs)
	}
}

func TestValidatePatch_MissingAnchor(t *testing.T) {
	edits := []map[string]any{
		{"op": "replace", "content": "new line"},
	}
	errs := ValidatePatch(edits)
	if len(errs) == 0 {
		t.Fatal("expected errors for missing anchor")
	}
	found := false
	for _, e := range errs {
		if e.Field == "anchor" && strings.Contains(e.Message, "required") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected anchor error mentioning required, got: %v", errs)
	}
}

func TestValidatePatch_EmptyAnchor(t *testing.T) {
	edits := []map[string]any{
		{"op": "replace", "anchor": "", "content": "new line"},
	}
	errs := ValidatePatch(edits)
	if len(errs) == 0 {
		t.Fatal("expected errors for empty anchor")
	}
	found := false
	for _, e := range errs {
		if e.Field == "anchor" && strings.Contains(e.Message, "required") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected anchor error, got: %v", errs)
	}
}

func TestValidatePatch_InvalidAnchorFormat(t *testing.T) {
	tests := []struct {
		name      string
		anchor    string
		wantField string
		wantMsg   string
	}{
		{
			name:      "no colon",
			anchor:    "nocolon",
			wantField: "anchor",
			wantMsg:   "invalid anchor format",
		},
		{
			name:      "hash too long",
			anchor:    "5:abc",
			wantField: "anchor",
			wantMsg:   "hash must be exactly 2 characters",
		},
		{
			name:      "hash empty",
			anchor:    "5:",
			wantField: "anchor",
			wantMsg:   "hash must be exactly 2 characters",
		},
		{
			name:      "non-numeric line",
			anchor:    "abc:xy",
			wantField: "anchor",
			wantMsg:   "invalid line number",
		},
		{
			name:      "tag too short",
			anchor:    "5:ab:xy",
			wantField: "anchor",
			wantMsg:   "snapshot tag must be exactly 4 hex characters",
		},
		{
			name:      "tag non-hex but correct length",
			anchor:    "5:xyza:ab",
			wantField: "anchor",
			wantMsg:   "", // passes validation (parser only checks length)
		},
		{
			name:      "tag too long",
			anchor:    "5:abcde:xy",
			wantField: "anchor",
			wantMsg:   "snapshot tag must be exactly 4 hex characters",
		},
		{
			name:      "tag wrong length and bad hash",
			anchor:    "5:abc:xyz",
			wantField: "anchor",
			wantMsg:   "hash must be exactly 2 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits := []map[string]any{
				{"op": "replace", "anchor": tt.anchor, "content": "x"},
			}
			errs := ValidatePatch(edits)
			if tt.wantMsg == "" {
				if len(errs) != 0 {
					t.Errorf("expected no errors for anchor %q, got: %v", tt.anchor, errs)
				}
				return
			}
			if len(errs) == 0 {
				t.Fatalf("expected errors for anchor %q, got none", tt.anchor)
			}
			found := false
			for _, e := range errs {
				if e.Field == tt.wantField && strings.Contains(e.Message, tt.wantMsg) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected error with field=%q msg containing %q, got: %v", tt.wantField, tt.wantMsg, errs)
			}
		})
	}
}

func TestValidatePatch_RangeInversion(t *testing.T) {
	edits := []map[string]any{
		{"op": "replace", "anchor": "10:ab", "end_anchor": "5:cd", "content": "x"},
	}
	errs := ValidatePatch(edits)
	if len(errs) == 0 {
		t.Fatal("expected errors for range inversion")
	}
	found := false
	for _, e := range errs {
		if e.Field == "end_anchor" && strings.Contains(e.Message, "before anchor line") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected range inversion error, got: %v", errs)
	}
}

func TestValidatePatch_ContentRequired(t *testing.T) {
	tests := []struct {
		name string
		op   string
	}{
		{"replace", "replace"},
		{"replace_block", "replace_block"},
		{"insert_after", "insert_after"},
		{"insert_before", "insert_before"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits := []map[string]any{
				{"op": tt.op, "anchor": "1:ab"},
			}
			errs := ValidatePatch(edits)
			if len(errs) == 0 {
				t.Fatalf("expected error for %s without content", tt.op)
			}
			found := false
			for _, e := range errs {
				if e.Field == "content" && strings.Contains(e.Message, "required") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected content-required error, got: %v", errs)
			}
		})
	}
}

func TestValidatePatch_ContentMustBeEmpty(t *testing.T) {
	tests := []struct {
		name    string
		op      string
		content string
	}{
		{"delete", "delete", "oops"},
		{"delete_block", "delete_block", "oops"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits := []map[string]any{
				{"op": tt.op, "anchor": "1:ab", "content": tt.content},
			}
			errs := ValidatePatch(edits)
			if len(errs) == 0 {
				t.Fatalf("expected error for %s with content", tt.op)
			}
			found := false
			for _, e := range errs {
				if e.Field == "content" && strings.Contains(e.Message, "must be empty") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected content-must-be-empty error, got: %v", errs)
			}
		})
	}
}

func TestValidatePatch_LineNumberBounds(t *testing.T) {
	tests := []struct {
		name    string
		anchor  string
		wantMsg string
	}{
		{"line zero", "0:ab", "must be > 0"},
		{"negative line", "-1:ab", "must be > 0"},
		{"line too large", "1000001:ab", "exceeds maximum"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits := []map[string]any{
				{"op": "replace", "anchor": tt.anchor, "content": "x"},
			}
			errs := ValidatePatch(edits)
			if len(errs) == 0 {
				t.Fatalf("expected error for anchor %q, got none", tt.anchor)
			}
			found := false
			for _, e := range errs {
				if e.Field == "anchor" && strings.Contains(e.Message, tt.wantMsg) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected anchor error mentioning %q, got: %v", tt.wantMsg, errs)
			}
		})
	}
}

func TestValidatePatch_MultipleErrors(t *testing.T) {
	edits := []map[string]any{
		{"op": "snip", "anchor": "abc:xyz", "content": "oops"},
		{"op": "replace", "anchor": "", "content": ""},
		{"op": "delete", "anchor": "5:ab", "content": "should be empty"},
	}
	errs := ValidatePatch(edits)
	// Should have multiple errors across the three edits
	if len(errs) < 4 {
		t.Errorf("expected at least 4 errors for patch with multiple violations, got %d: %v", len(errs), errs)
	}
	// Verify edit indices are 1-based
	seenIndices := make(map[int]bool)
	for _, e := range errs {
		if e.EditIndex < 1 || e.EditIndex > 3 {
			t.Errorf("edit index %d out of range [1,3]", e.EditIndex)
		}
		seenIndices[e.EditIndex] = true
	}
	if len(seenIndices) < 2 {
		t.Errorf("expected errors across multiple edits, only saw indices: %v", seenIndices)
	}
}

func TestValidatePatchFromOps(t *testing.T) {
	tests := []struct {
		name     string
		ops      []editOp
		wantErrs bool
		wantMsg  string
	}{
		{
			name: "valid ops pass",
			ops: []editOp{
				{Op: "replace", Anchor: "10:ab", Content: "new line"},
				{Op: "insert_after", Anchor: "BOF", Content: "first"},
				{Op: "delete", Anchor: "5:cd"},
			},
			wantErrs: false,
		},
		{
			name:     "invalid op",
			ops:      []editOp{{Op: "snip", Anchor: "1:ab"}},
			wantErrs: true,
			wantMsg:  "'snip'",
		},
		{
			name:     "missing anchor",
			ops:      []editOp{{Op: "replace", Content: "x"}},
			wantErrs: true,
			wantMsg:  "required",
		},
		{
			name:     "bad anchor format",
			ops:      []editOp{{Op: "replace", Anchor: "nocolon", Content: "x"}},
			wantErrs: true,
			wantMsg:  "invalid anchor format",
		},
		{
			name:     "range inversion",
			ops:      []editOp{{Op: "replace", Anchor: "10:ab", EndAnchor: "5:cd", Content: "x"}},
			wantErrs: true,
			wantMsg:  "before anchor line",
		},
		{
			name:     "replace without content",
			ops:      []editOp{{Op: "replace", Anchor: "1:ab"}},
			wantErrs: true,
			wantMsg:  "required",
		},
		{
			name:     "delete with content",
			ops:      []editOp{{Op: "delete", Anchor: "1:ab", Content: "oops"}},
			wantErrs: true,
			wantMsg:  "must be empty",
		},
		{
			name:     "line number zero",
			ops:      []editOp{{Op: "replace", Anchor: "0:ab", Content: "x"}},
			wantErrs: true,
			wantMsg:  "must be > 0",
		},
		{
			name: "multiple errors across ops",
			ops: []editOp{
				{Op: "snip", Anchor: "abc:xyz", Content: "oops"},
				{Op: "delete", Anchor: "5:ab", Content: "bad"},
			},
			wantErrs: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidatePatchFromOps(tt.ops)
			if tt.wantErrs {
				if len(errs) == 0 {
					t.Fatalf("expected errors, got none for ops: %+v", tt.ops)
				}
				if tt.wantMsg != "" {
					found := false
					for _, e := range errs {
						if strings.Contains(e.Message, tt.wantMsg) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected error message containing %q, got: %v", tt.wantMsg, errs)
					}
				}
			} else {
				if errs != nil {
					t.Errorf("expected no errors, got: %v", errs)
				}
			}
		})
	}
}

func TestPatchError_String(t *testing.T) {
	e := PatchError{EditIndex: 3, Field: "anchor", Message: "hash must be exactly 2 characters"}
	want := "edit 3 anchor: hash must be exactly 2 characters"
	got := e.String()
	if got != want {
		t.Errorf("PatchError.String() = %q, want %q", got, want)
	}
}

func TestPatchError_EditIndexFormat(t *testing.T) {
	// Ensure all error messages are lowercase per project convention
	edits := []map[string]any{
		{"op": "snip", "anchor": "abc:xyz", "content": "oops"},
	}
	errs := ValidatePatch(edits)
	for _, e := range errs {
		msg := e.String()
		if msg != strings.ToLower(msg) {
			t.Errorf("error message not lowercase: %q", msg)
		}
	}
}

func TestValidatePatch_EndAnchorInvalidFormat(t *testing.T) {
	edits := []map[string]any{
		{"op": "replace", "anchor": "1:ab", "end_anchor": "badformat", "content": "x"},
	}
	errs := ValidatePatch(edits)
	if len(errs) == 0 {
		t.Fatal("expected errors for invalid end_anchor format")
	}
	found := false
	for _, e := range errs {
		if e.Field == "end_anchor" && strings.Contains(e.Message, "invalid anchor format") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected end_anchor format error, got: %v", errs)
	}
}

func TestValidatePatch_EmptyEdits(t *testing.T) {
	errs := ValidatePatch(nil)
	if errs != nil {
		t.Errorf("expected nil for empty edits, got: %v", errs)
	}
	errs = ValidatePatch([]map[string]any{})
	if errs != nil {
		t.Errorf("expected nil for empty edits slice, got: %v", errs)
	}
}

// Test that the error format integration works as expected by the caller.
func TestValidatePatch_ErrorFormatting(t *testing.T) {
	edits := []map[string]any{
		{"op": "snip", "anchor": "0:abc"},
		{"op": "delete", "anchor": "5:de", "content": "bad content"},
	}
	errs := ValidatePatch(edits)
	if len(errs) == 0 {
		t.Fatal("expected errors")
	}

	// Format errors the same way the caller would
	var msgs []string
	for _, e := range errs {
		msgs = append(msgs, fmt.Sprintf("edit %d %s: %s", e.EditIndex, e.Field, e.Message))
	}
	joined := strings.Join(msgs, "\n")

	// Verify the combined message looks reasonable
	if !strings.Contains(joined, "edit 1") {
		t.Error("formatted errors should contain 'edit 1'")
	}
	if !strings.Contains(joined, "edit 2") {
		t.Error("formatted errors should contain 'edit 2'")
	}

	// Verify all messages are lowercase
	for _, msg := range msgs {
		if msg != strings.ToLower(msg) {
			t.Errorf("error message not lowercase: %q", msg)
		}
	}
}
