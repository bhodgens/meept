package daemon

import (
	"github.com/caimlas/meept/internal/llm"
)

// scratchRigRunDirs returns the run dirs of meept scratch rigs (e2e / bench
// harness homes under the OS temp dir) for the boot orphan sweep. The
// discovery itself lives in internal/llm next to the spawn-record format it
// reads.
func scratchRigRunDirs() []string {
	return llm.ListScratchRigRunDirs()
}
