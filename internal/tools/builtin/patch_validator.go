package builtin

import (
	"fmt"
	"strconv"
	"strings"
)

// PatchError represents a validation error at a specific location in the patch.
type PatchError struct {
	EditIndex int    // 1-based edit index
	Field     string // "op", "anchor", "end_anchor", "content"
	Message   string // human-readable error
}

// String returns a human-readable representation of the patch error.
func (e PatchError) String() string {
	return fmt.Sprintf("edit %d %s: %s", e.EditIndex, e.Field, e.Message)
}

// validOps is the set of allowed operation names.
var validOps = map[string]bool{
	"replace":       true,
	"replace_block": true,
	"insert_after":  true,
	"insert_before": true,
	"delete":        true,
	"delete_block":  true,
}

// opsRequiringContent are operations that need non-empty content.
var opsRequiringContent = map[string]bool{
	"replace":       true,
	"replace_block": true,
	"insert_after":  true,
	"insert_before": true,
}

// opsForbiddingContent are operations that must have empty content.
var opsForbiddingContent = map[string]bool{
	"delete":       true,
	"delete_block": true,
}

const maxLineNum = 1000000

// ValidatePatch validates an array of edit operations (raw maps) against the patch grammar.
// Returns nil if valid, or a slice of PatchError for all violations found.
func ValidatePatch(edits []map[string]any) []PatchError {
	var errs []PatchError

	for i, raw := range edits {
		idx := i + 1 // 1-based
		errs = append(errs, validateEditMap(idx, raw)...)
	}

	if len(errs) == 0 {
		return nil
	}
	return errs
}

// ValidatePatchFromOps validates an already-parsed slice of editOp structs.
// Returns nil if valid, or a slice of PatchError for all violations found.
func ValidatePatchFromOps(ops []editOp) []PatchError {
	var errs []PatchError

	for i, op := range ops {
		idx := i + 1 // 1-based
		errs = append(errs, validateEditOp(idx, op)...)
	}

	if len(errs) == 0 {
		return nil
	}
	return errs
}

// validateEditMap validates a single raw edit map from JSON input.
func validateEditMap(idx int, m map[string]any) []PatchError {
	op, _ := m["op"].(string)
	anchor, _ := m["anchor"].(string)
	endAnchor, _ := m["end_anchor"].(string)
	content, _ := m["content"].(string)

	return validateCommon(idx, op, anchor, endAnchor, content)
}

// validateEditOp validates a single parsed editOp struct.
func validateEditOp(idx int, op editOp) []PatchError {
	return validateCommon(idx, op.Op, op.Anchor, op.EndAnchor, op.Content)
}

// validateCommon contains the shared validation logic for both raw maps and parsed ops.
func validateCommon(idx int, op, anchor, endAnchor, content string) []PatchError {
	var errs []PatchError

	// 1. Validate op
	if !validOps[op] {
		errs = append(errs, PatchError{
			EditIndex: idx,
			Field:     "op",
			Message:   fmt.Sprintf("op must be one of: replace, replace_block, insert_after, insert_before, delete, delete_block, got '%s'", op),
		})
	}

	// 2. Validate anchor presence
	if anchor == "" {
		errs = append(errs, PatchError{
			EditIndex: idx,
			Field:     "anchor",
			Message:   "anchor is required and must be non-empty",
		})
		return errs // nothing more to validate without an anchor
	}

	// 3. Validate anchor format
	anchorLineNum := validateAnchorFormat(idx, "anchor", anchor, &errs)

	// 4. Validate end_anchor format (if present)
	var endLineNum int
	if endAnchor != "" {
		endLineNum = validateAnchorFormat(idx, "end_anchor", endAnchor, &errs)
	}

	// 5. Validate range sanity
	if anchorLineNum > 0 && endLineNum > 0 && endLineNum < anchorLineNum {
		errs = append(errs, PatchError{
			EditIndex: idx,
			Field:     "end_anchor",
			Message:   fmt.Sprintf("end_anchor line %d is before anchor line %d", endLineNum, anchorLineNum),
		})
	}

	// 6. Validate content requirements based on op
	if validOps[op] {
		if opsRequiringContent[op] && content == "" {
			errs = append(errs, PatchError{
				EditIndex: idx,
				Field:     "content",
				Message:   fmt.Sprintf("content is required for op '%s'", op),
			})
		}
		if opsForbiddingContent[op] && content != "" {
			errs = append(errs, PatchError{
				EditIndex: idx,
				Field:     "content",
				Message:   fmt.Sprintf("content must be empty for op '%s', got %d bytes", op, len(content)),
			})
		}
	}

	return errs
}

// validateAnchorFormat validates the format of an anchor string.
// Returns the line number if parseable (positive), or 0 for BOF/EOF/unparseable.
func validateAnchorFormat(idx int, field, anchor string, errs *[]PatchError) int {
	// BOF and EOF are valid special anchors
	if anchor == "BOF" || anchor == "EOF" {
		return 0
	}

	parts := strings.SplitN(anchor, ":", 3)

	switch len(parts) {
	case 1:
		// No colons at all -- unknown format
		*errs = append(*errs, PatchError{
			EditIndex: idx,
			Field:     field,
			Message:   fmt.Sprintf("invalid anchor format '%s', expected LINE:HASH or LINE:TAG:HASH", anchor),
		})
		return 0

	case 2:
		// Legacy format: LINE:HASH
		lineNum, parseErr := strconv.Atoi(parts[0])
		if parseErr != nil {
			*errs = append(*errs, PatchError{
				EditIndex: idx,
				Field:     field,
				Message:   fmt.Sprintf("invalid line number in anchor '%s': not a valid integer", anchor),
			})
			return 0
		}
		hash := parts[1]
		if len(hash) != 2 {
			*errs = append(*errs, PatchError{
				EditIndex: idx,
				Field:     field,
				Message:   fmt.Sprintf("hash must be exactly 2 characters, got '%s'", hash),
			})
		}
		validateLineNumber(idx, field, anchor, lineNum, errs)
		return lineNum

	case 3:
		// Full format: LINE:TAG:HASH
		lineNum, parseErr := strconv.Atoi(parts[0])
		if parseErr != nil {
			*errs = append(*errs, PatchError{
				EditIndex: idx,
				Field:     field,
				Message:   fmt.Sprintf("invalid line number in anchor '%s': not a valid integer", anchor),
			})
			return 0
		}
		tag := parts[1]
		hash := parts[2]
		if len(tag) != 4 {
			*errs = append(*errs, PatchError{
				EditIndex: idx,
				Field:     field,
				Message:   fmt.Sprintf("snapshot tag must be exactly 4 hex characters, got '%s'", tag),
			})
		}
		if len(hash) != 2 {
			*errs = append(*errs, PatchError{
				EditIndex: idx,
				Field:     field,
				Message:   fmt.Sprintf("hash must be exactly 2 characters, got '%s'", hash),
			})
		}
		validateLineNumber(idx, field, anchor, lineNum, errs)
		return lineNum

	default:
		// SplitN with 3 should never produce >3 parts
		*errs = append(*errs, PatchError{
			EditIndex: idx,
			Field:     field,
			Message:   fmt.Sprintf("invalid anchor format '%s'", anchor),
		})
		return 0
	}
}

// validateLineNumber checks line number sanity bounds.
func validateLineNumber(idx int, field, anchor string, lineNum int, errs *[]PatchError) {
	if lineNum < 1 {
		*errs = append(*errs, PatchError{
			EditIndex: idx,
			Field:     field,
			Message:   fmt.Sprintf("anchor line number %d must be > 0", lineNum),
		})
	} else if lineNum > maxLineNum {
		*errs = append(*errs, PatchError{
			EditIndex: idx,
			Field:     field,
			Message:   fmt.Sprintf("anchor line number %d exceeds maximum %d", lineNum, maxLineNum),
		})
	}
}
