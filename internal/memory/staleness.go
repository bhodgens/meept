package memory

import (
	"fmt"
	"time"
)

// MemoryFreshnessText returns a staleness caveat for memories older than one
// day. Fresh memories (age <= 1 day) return an empty string.
func MemoryFreshnessText(ageDays int) string {
	if ageDays <= 1 {
		return ""
	}
	return fmt.Sprintf(
		"This memory is %d days old. Memories are point-in-time observations, not live state — "+
			"claims about code behavior or file:line citations may be outdated. "+
			"Verify against current code before asserting as fact.",
		ageDays,
	)
}

// FormatMemoryWithFreshness appends a staleness caveat to memory content when
// the memory was last updated more than one day ago. If updatedAt is nil the
// content is returned unchanged.
func FormatMemoryWithFreshness(content string, updatedAt *time.Time) string {
	if updatedAt == nil {
		return content
	}
	ageDays := int(time.Since(*updatedAt).Hours() / 24)
	caveat := MemoryFreshnessText(ageDays)
	if caveat == "" {
		return content
	}
	return content + "\n\n[" + caveat + "]"
}
