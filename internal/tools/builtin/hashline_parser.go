package builtin

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// hashlinePatchGrammar is the formal EBNF grammar for the hashline patch language.
// This grammar defines the syntax of edit operations used by the file_edit tool.
// It can be provided to LLMs for constrained decoding or structural guidance.
const hashlinePatchGrammar = `(* hashline patch language grammar

   Describes the JSON array of edit operations consumed by the file_edit tool.
   Each edit targets a line or range of lines identified by hashline anchors.

   Anchors:
     LINE:HASH          — legacy format, two-char lowercase bigram
     LINE:TAG:HASH      — tagged format, four-hex-char snapshot tag
     BOF                — beginning of file
     EOF                — end of file
*)

Patch       ::= Edit+
Edit        ::= Op Anchor [EndAnchor] [Tag] [Content]
Op          ::= "replace" | "replace_block" | "insert_after" | "insert_before" | "delete" | "delete_block"
Anchor      ::= LineAnchor | "BOF" | "EOF"
LineAnchor  ::= INT ":" HASH | INT ":" TAG ":" HASH
EndAnchor   ::= LineAnchor
Tag         ::= HEX4
Content     ::= STRING

(* lexical tokens *)
INT         ::= [1-9] [0-9]*
HASH        ::= ALPHA2
TAG         ::= HEX4
ALPHA2      ::= [a-z] [a-z]
HEX4        ::= [0-9a-f] [0-9a-f] [0-9a-f] [0-9a-f]
STRING      ::= any valid JSON string
`

// AnchorType classifies an anchor as a line reference, BOF, or EOF.
type AnchorType int

const (
	// AnchorTypeLine is a numeric line reference (LINE:HASH or LINE:TAG:HASH).
	AnchorTypeLine AnchorType = iota
	// AnchorTypeBOF is the beginning-of-file sentinel.
	AnchorTypeBOF
	// AnchorTypeEOF is the end-of-file sentinel.
	AnchorTypeEOF
)

// String returns a human-readable label for the anchor type.
func (at AnchorType) String() string {
	switch at {
	case AnchorTypeLine:
		return "line"
	case AnchorTypeBOF:
		return "bof"
	case AnchorTypeEOF:
		return "eof"
	default:
		return "unknown"
	}
}

// ParsedEdit is a fully validated edit operation with all anchor components
// decomposed into typed fields. This is the output of PatchParser.ParseEdit.
type ParsedEdit struct {
	// Op is the validated operation name (e.g. "replace", "delete").
	Op string

	// --- primary anchor ---
	AnchorType  AnchorType
	AnchorLine  int    // 0 for BOF/EOF
	AnchorHash  string // 2-char bigram (empty for BOF/EOF)
	AnchorTag   string // 4-char hex snapshot tag (empty for legacy format or BOF/EOF)

	// --- optional end anchor (for range ops) ---
	EndAnchorType  AnchorType
	EndAnchorLine  int
	EndAnchorHash  string
	EndAnchorTag   string

	// Content is the replacement text for replace/insert ops (empty for delete).
	Content string

	// SnapshotTag is an optional tag provided via the "tag" field.
	SnapshotTag string
}

// parsedAnchor holds the decomposed components of a single anchor string.
type parsedAnchor struct {
	aType AnchorType
	line  int
	hash  string
	tag   string
}

// Precompiled regexes for anchor format validation.
var (
	// lineAnchorLegacy matches LINE:HASH (e.g. "10:ab").
	lineAnchorLegacy = regexp.MustCompile(`^(\d+):([a-z]{2})$`)
	// lineAnchorTagged matches LINE:TAG:HASH (e.g. "10:a1b2:ab").
	lineAnchorTagged = regexp.MustCompile(`^(\d+):([0-9a-f]{4}):([a-z]{2})$`)
	// hex4Pattern matches a 4-character hex string.
	hex4Pattern = regexp.MustCompile(`^[0-9a-f]{4}$`)
)

// PatchParser provides grammar-based parsing and validation of hashline patch
// operations. It produces ParsedEdit structs from raw JSON maps and returns
// PatchError diagnostics for any violations of the grammar.
//
// The parser validates the same invariants as patch_validator.go (ValidatePatch)
// but from a grammar-first approach, making it suitable for structured decoding
// and LLM constrained output schemas.
type PatchParser struct{}

// Grammar returns the formal EBNF grammar for the hashline patch language.
// This can be embedded in prompts or tool descriptions for LLM guidance.
func (p *PatchParser) Grammar() string {
	return hashlinePatchGrammar
}

// GrammarForConstrainedDecoding returns the patch grammar expressed as a JSON
// Schema fragment suitable for LLM constrained/structured output (e.g. OpenAI
// function calling, Anthropic tool_use, or JSON Schema mode). The schema
// describes the shape of a single edit object within the "edits" array.
func (p *PatchParser) GrammarForConstrainedDecoding() string {
	// Build schema with deterministic key ordering using orderedMap.
	anchorProps := omSet(
		"oneOf", []any{
			om("type", "string", "pattern", `^[1-9][0-9]*:[a-z]{2}$`, "description", "line anchor in legacy format (LINE:HASH)"),
			om("type", "string", "pattern", `^[1-9][0-9]*:[0-9a-f]{4}:[a-z]{2}$`, "description", "line anchor with snapshot tag (LINE:TAG:HASH)"),
			om("enum", []string{"BOF"}, "description", "beginning of file"),
			om("enum", []string{"EOF"}, "description", "end of file"),
		},
	)

	editSchema := om(
		"type", "object",
		"properties", om(
			"op", om("type", "string", "enum", []string{"replace", "replace_block", "insert_after", "insert_before", "delete", "delete_block"}, "description", "edit operation type"),
			"anchor", anchorProps,
			"end_anchor", om("type", "string", "pattern", `^[1-9][0-9]*:[a-z]{2}$|^[1-9][0-9]*:[0-9a-f]{4}:[a-z]{2}$`, "description", "optional end anchor for range operations (LINE:HASH or LINE:TAG:HASH)"),
			"content", om("type", "string", "description", "replacement content for replace/insert operations (must be empty for delete ops)"),
			"tag", om("type", "string", "pattern", `^[0-9a-f]{4}$`, "description", "optional 4-hex-char snapshot tag for cache lookup"),
		),
		"required", []string{"op", "anchor"},
		"additionalProperties", false,
	)

	b, err := json.MarshalIndent(editSchema, "", "  ")
	if err != nil {
		// Should never happen with static data.
		return "{}"
	}
	return string(b)
}

// om creates an orderedMap from alternating key-value pairs.
// Panics if the number of arguments is odd (programming error).
func om(kv ...any) *orderedMap {
	if len(kv)%2 != 0 {
		panic(fmt.Sprintf("om: odd number of key-value arguments: %d", len(kv)))
	}
	omVal := newOrderedMap()
	for i := 0; i < len(kv); i += 2 {
		key, _ := kv[i].(string)
		omVal.Set(key, kv[i+1])
	}
	return omVal
}

// omSet creates an orderedMap with a single key-value entry.
func omSet(key string, val any) *orderedMap {
	omVal := newOrderedMap()
	omVal.Set(key, val)
	return omVal
}

// ParsePatch parses a slice of raw JSON edit maps into validated ParsedEdit
// structs. It returns all edits that parsed successfully (even if other edits
// had errors) and all accumulated validation errors.
// If no errors are found, the returned error slice is nil (not empty).
func (p *PatchParser) ParsePatch(input []map[string]any) ([]ParsedEdit, []PatchError) {
	if len(input) == 0 {
		return nil, nil
	}

	var (
		edits []ParsedEdit
		errs  []PatchError
	)

	for i, raw := range input {
		edit, editErrs := p.ParseEdit(i+1, raw)
		if edit != nil {
			edits = append(edits, *edit)
		}
		errs = append(errs, editErrs...)
	}

	if len(errs) == 0 {
		return edits, nil
	}
	return edits, errs
}

// ParseEdit parses a single raw JSON edit map into a validated ParsedEdit.
// idx is the 1-based edit index used in error reporting.
// Returns nil edit if the input cannot be parsed at all.
func (p *PatchParser) ParseEdit(idx int, raw map[string]any) (*ParsedEdit, []PatchError) {
	var errs []PatchError

	// Extract raw string fields with type-safe fallbacks.
	op, _ := raw["op"].(string)
	anchor, _ := raw["anchor"].(string)
	endAnchor, _ := raw["end_anchor"].(string)
	content, _ := raw["content"].(string)
	tag, _ := raw["tag"].(string)

	// 1. Validate op.
	if !validOps[op] {
		errs = append(errs, PatchError{
			EditIndex: idx,
			Field:     "op",
			Message: fmt.Sprintf(
				"op must be one of: replace, replace_block, insert_after, insert_before, delete, delete_block, got '%s'",
				op,
			),
		})
	}

	// 2. Validate anchor presence.
	if anchor == "" {
		errs = append(errs, PatchError{
			EditIndex: idx,
			Field:     "anchor",
			Message:   "anchor is required and must be non-empty",
		})
	}

	// 3. Parse and validate primary anchor.
	var primaryAnchor parsedAnchor
	if anchor != "" {
		primaryAnchor = p.parseAnchor(idx, "anchor", anchor, &errs)
	}

	// 4. Parse and validate end anchor (if present).
	var endAnc parsedAnchor
	if endAnchor != "" {
		endAnc = p.parseAnchor(idx, "end_anchor", endAnchor, &errs)
	}

	// 5. Validate range sanity: end_anchor line must not precede anchor line.
	if primaryAnchor.line > 0 && endAnc.line > 0 && endAnc.line < primaryAnchor.line {
		errs = append(errs, PatchError{
			EditIndex: idx,
			Field:     "end_anchor",
			Message: fmt.Sprintf(
				"end_anchor line %d is before anchor line %d",
				endAnc.line, primaryAnchor.line,
			),
		})
	}

	// 6. Validate content requirements based on op.
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
				Message: fmt.Sprintf(
					"content must be empty for op '%s', got %d bytes",
					op, len(content),
				),
			})
		}
	}

	// 7. Validate optional tag format.
	if tag != "" && !hex4Pattern.MatchString(tag) {
		errs = append(errs, PatchError{
			EditIndex: idx,
			Field:     "tag",
			Message: fmt.Sprintf(
				"tag must be exactly 4 hex characters, got '%s'",
				tag,
			),
		})
	}

	// Build the ParsedEdit from parsed components.
	edit := &ParsedEdit{
		Op:         op,
		AnchorType: primaryAnchor.aType,
		AnchorLine: primaryAnchor.line,
		AnchorHash: primaryAnchor.hash,
		AnchorTag:  primaryAnchor.tag,
		EndAnchorType: endAnc.aType,
		EndAnchorLine: endAnc.line,
		EndAnchorHash: endAnc.hash,
		EndAnchorTag:  endAnc.tag,
		Content:      content,
		SnapshotTag:  tag,
	}

	return edit, errs
}

// parseAnchor decomposes an anchor string into its typed components, appending
// validation errors to errs as needed. field is "anchor" or "end_anchor".
func (p *PatchParser) parseAnchor(idx int, field, anchor string, errs *[]PatchError) parsedAnchor {
	// Special sentinels.
	switch anchor {
	case "BOF":
		return parsedAnchor{aType: AnchorTypeBOF}
	case "EOF":
		return parsedAnchor{aType: AnchorTypeEOF}
	}

	// Try tagged format first (LINE:TAG:HASH), then legacy (LINE:HASH).
	if m := lineAnchorTagged.FindStringSubmatch(anchor); m != nil {
		lineNum, err := strconv.Atoi(m[1])
		if err != nil {
			// Shouldn't happen since regex requires digits, but be safe.
			*errs = append(*errs, PatchError{
				EditIndex: idx,
				Field:     field,
				Message:   fmt.Sprintf("invalid line number in anchor '%s': not a valid integer", anchor),
			})
			return parsedAnchor{}
		}
		validateLineNumber(idx, field, anchor, lineNum, errs)
		return parsedAnchor{
			aType: AnchorTypeLine,
			line:  lineNum,
			tag:   m[2],
			hash:  m[3],
		}
	}

	if m := lineAnchorLegacy.FindStringSubmatch(anchor); m != nil {
		lineNum, err := strconv.Atoi(m[1])
		if err != nil {
			*errs = append(*errs, PatchError{
				EditIndex: idx,
				Field:     field,
				Message:   fmt.Sprintf("invalid line number in anchor '%s': not a valid integer", anchor),
			})
			return parsedAnchor{}
		}
		validateLineNumber(idx, field, anchor, lineNum, errs)
		return parsedAnchor{
			aType: AnchorTypeLine,
			line:  lineNum,
			hash:  m[2],
		}
	}

	// No format matched.
	parts := strings.SplitN(anchor, ":", 3)
	switch len(parts) {
	case 1:
		*errs = append(*errs, PatchError{
			EditIndex: idx,
			Field:     field,
			Message: fmt.Sprintf(
				"invalid anchor format '%s', expected LINE:HASH or LINE:TAG:HASH",
				anchor,
			),
		})
	default:
		// Has colons but doesn't match either format. Report the most specific
		// error based on the split structure.
		if len(parts) == 3 {
			lineNum, parseErr := strconv.Atoi(parts[0])
			if parseErr != nil {
				*errs = append(*errs, PatchError{
					EditIndex: idx,
					Field:     field,
					Message: fmt.Sprintf(
						"invalid line number in anchor '%s': not a valid integer",
						anchor,
					),
				})
				return parsedAnchor{}
			}
			tag := parts[1]
			hash := parts[2]
			if len(tag) != 4 {
				*errs = append(*errs, PatchError{
					EditIndex: idx,
					Field:     field,
					Message: fmt.Sprintf(
						"snapshot tag must be exactly 4 hex characters, got '%s'",
						tag,
					),
				})
			}
			if len(hash) != 2 {
				*errs = append(*errs, PatchError{
					EditIndex: idx,
					Field:     field,
					Message: fmt.Sprintf(
						"hash must be exactly 2 characters, got '%s'",
						hash,
					),
				})
			}
			validateLineNumber(idx, field, anchor, lineNum, errs)
		} else {
			// 2 parts: LINE:something (legacy format mismatch)
			*errs = append(*errs, PatchError{
				EditIndex: idx,
				Field:     field,
				Message: fmt.Sprintf(
					"invalid anchor format '%s', expected LINE:HASH or LINE:TAG:HASH",
					anchor,
				),
			})
		}
	}

	return parsedAnchor{}
}

// orderedMap is a key-value container that preserves insertion order during
// JSON marshaling. This is used to produce deterministic, human-readable JSON
// Schema output. It uses an internal slice of key-value pairs alongside a map
// for O(1) lookups.
type orderedMap struct {
	keys   []string
	values map[string]any
}

// newOrderedMap creates a new empty orderedMap.
func newOrderedMap() *orderedMap {
	return &orderedMap{
		values: make(map[string]any),
	}
}

// Set adds or updates a key in the orderedMap. If the key is new, it is
// appended to the key order. If it already exists, the value is updated
// without changing the order.
func (om *orderedMap) Set(key string, val any) {
	if om.values == nil {
		om.values = make(map[string]any)
	}
	if _, exists := om.values[key]; !exists {
		om.keys = append(om.keys, key)
	}
	om.values[key] = val
}

// Get retrieves a value by key. Returns nil and false if not found.
func (om *orderedMap) Get(key string) (any, bool) {
	if om == nil || om.values == nil {
		return nil, false
	}
	v, ok := om.values[key]
	return v, ok
}

// MarshalJSON serializes the orderedMap in insertion order, producing
// deterministic JSON output.
func (om *orderedMap) MarshalJSON() ([]byte, error) {
	var buf strings.Builder
	buf.WriteByte('{')
	for i, k := range om.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		keyBytes, err := json.Marshal(k)
		if err != nil {
			return nil, fmt.Errorf("marshal key %q: %w", k, err)
		}
		buf.Write(keyBytes)
		buf.WriteByte(':')
		valBytes, err := json.Marshal(om.values[k])
		if err != nil {
			return nil, fmt.Errorf("marshal value for key %q: %w", k, err)
		}
		buf.Write(valBytes)
	}
	buf.WriteByte('}')
	return []byte(buf.String()), nil
}

// Ensure PatchParser satisfies a basic interface contract.
var (
	_ interface{ Grammar() string }                      = (*PatchParser)(nil)
	_ interface{ GrammarForConstrainedDecoding() string } = (*PatchParser)(nil)
)
