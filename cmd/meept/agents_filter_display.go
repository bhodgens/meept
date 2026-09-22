package main

import (
	"fmt"
	"sort"
	"strings"
)

// renderOutputFiltersSection renders the `output filters:` section of
// `meept agents show` (output-filters tree, leaf 04 Task 4), following the
// Verification: section pattern. All values are lowercase per the repo UI
// convention.
//
// Enabled shape:
//
//	output filters: enabled, filters: json_format, language_en, max passes: 2, retries: 2
//
// Disabled shape:
//
//	output filters: off (daemon default disabled)
func renderOutputFiltersSection(m map[string]any) string {
	enabled := truthy(m, "enabled")
	if !enabled {
		return "\noutput filters: off (daemon default disabled)"
	}

	filters := filterNames(m["filters"])
	maxPasses := intValue(m["max_passes"])
	retries := intValue(m["max_filter_retries"])

	var b strings.Builder
	b.WriteString("\noutput filters: enabled")
	if len(filters) > 0 {
		b.WriteString(", filters: ")
		b.WriteString(strings.Join(filters, ", "))
	}
	b.WriteString(fmt.Sprintf(", max passes: %d, retries: %d", maxPasses, retries))
	return b.String()
}

// truthy reports whether the JSON-decoded value is a true boolean.
func truthy(m map[string]any, key string) bool {
	v, ok := m[key].(bool)
	return ok && v
}

// filterNames coerces the JSON-decoded filter list to strings, sorted
// case-insensitively for stable display; empty/missing yields nil.
func filterNames(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			names = append(names, s)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return names
}

// intValue coerces the JSON-decoded number (float64 from encoding/json)
// to an int; missing/non-numeric yields 0.
func intValue(v any) int {
	f, ok := v.(float64)
	if !ok {
		return 0
	}
	return int(f)
}
