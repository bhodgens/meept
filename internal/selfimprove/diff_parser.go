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
	hasOriginalMarker := false
	hasSeparator := false
	hasFixedMarker := false

	for _, line := range lines {
		if strings.HasPrefix(line, "<<<<<<< ORIGINAL") {
			inOriginal = true
			hasOriginalMarker = true
			continue
		}
		if line == "=======" {
			inOriginal = false
			inFixed = true
			hasSeparator = true
			continue
		}
		if strings.HasPrefix(line, ">>>>>>> FIXED") {
			inFixed = false
			hasFixedMarker = true
			continue
		}

		if inOriginal {
			origLines = append(origLines, line)
		}
		if inFixed {
			fixedLines = append(fixedLines, line)
		}
	}

	if !hasOriginalMarker || !hasSeparator || !hasFixedMarker {
		return "", "", fmt.Errorf("malformed diff: missing required markers")
	}

	if len(origLines) == 0 && len(fixedLines) == 0 {
		return "", "", fmt.Errorf("could not parse diff")
	}

	return strings.Join(origLines, "\n"), strings.Join(fixedLines, "\n"), nil
}
