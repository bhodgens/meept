package selfimprove

import (
	"fmt"
	"strings"
)

// parseDiff parses a conflict-style diff containing <<<<<<< ORIGINAL,
// =======, and >>>>>>> FIXED markers and returns the original and fixed
// code blocks.
func parseDiff(diff string) (original, fixed string, err error) {
	lines := strings.Split(diff, "\n")
	inOriginal := false
	inFixed := false
	var origLines, fixedLines []string

	for _, line := range lines {
		if strings.HasPrefix(line, "<<<<<<< ORIGINAL") {
			inOriginal = true
			continue
		}
		if line == "=======" {
			inOriginal = false
			inFixed = true
			continue
		}
		if strings.HasPrefix(line, ">>>>>>> FIXED") {
			inFixed = false
			continue
		}

		if inOriginal {
			origLines = append(origLines, line)
		}
		if inFixed {
			fixedLines = append(fixedLines, line)
		}
	}

	if len(origLines) == 0 && len(fixedLines) == 0 {
		return "", "", fmt.Errorf("could not parse diff")
	}

	return strings.Join(origLines, "\n"), strings.Join(fixedLines, "\n"), nil
}
