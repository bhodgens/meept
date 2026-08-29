package eval

import (
	"crypto/sha256"
	"encoding/hex"
)

// HarnessHash derives a stable identity for an eval harness from its prompt,
// tool list, and gate command. It is the sha256 hex digest of
// sha256(prompt ++ NUL ++ toolList ++ NUL ++ gateCommand); the inner digest
// prevents trivial length-extension ambiguities between concatenated inputs.
func HarnessHash(prompt, toolList, gateCommand string) string {
	inner := sha256.Sum256([]byte(prompt + "\x00" + toolList + "\x00" + gateCommand))
	outer := sha256.Sum256(inner[:])
	return hex.EncodeToString(outer[:])
}
