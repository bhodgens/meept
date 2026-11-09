package builtin

import (
	"strings"
	"testing"
)

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
