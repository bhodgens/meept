//go:build e2e

package chatidentity

import (
	"github.com/caimlas/meept/e2e/harness"
)

// boot_test.go pins that the suite's sandbox stack boots cleanly (harness
// Start with the suite's config overlay). The scenario tests in
// chat_identity_test.go + submit_identity_test.go each call harness.Start
// through the shared helpers in helpers_test.go; this file keeps the boot
// concern documented and the harness import anchored for the package.
var _ = harness.Start
