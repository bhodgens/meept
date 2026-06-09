package tools

import (
	"fmt"
	"strings"
)

// generateSimpleDiff generates a simple unified diff between original and modified content.
func generateSimpleDiff(original, modified, filePath string) string {
	origLines := strings.Split(original, "\n")
	modLines := strings.Split(modified, "\n")

	// Handle trailing newline
	if len(origLines) > 0 && origLines[len(origLines)-1] == "" {
		origLines = origLines[:len(origLines)-1]
	}
	if len(modLines) > 0 && modLines[len(modLines)-1] == "" {
		modLines = modLines[:len(modLines)-1]
	}

	var diff []string
	diff = append(diff, fmt.Sprintf("--- a/%s", filePath))
	diff = append(diff, fmt.Sprintf("+++ b/%s", filePath))

	maxLen := len(origLines)
	if len(modLines) > maxLen {
		maxLen = len(modLines)
	}

	for i := 0; i < maxLen; i++ {
		oldLine := ""
		newLine := ""
		if i < len(origLines) {
			oldLine = origLines[i]
		}
		if i < len(modLines) {
			newLine = modLines[i]
		}

		if i >= len(origLines) {
			diff = append(diff, fmt.Sprintf("+%s", newLine))
		} else if i >= len(modLines) {
			diff = append(diff, fmt.Sprintf("-%s", oldLine))
		} else if oldLine != newLine {
			diff = append(diff, fmt.Sprintf("-%s", oldLine))
			diff = append(diff, fmt.Sprintf("+%s", newLine))
		}
	}

	return strings.Join(diff, "\n")
}
