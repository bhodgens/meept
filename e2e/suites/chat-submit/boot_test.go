//go:build e2e

package chatsubmit

import (
	"crypto/tls"
	"testing"

	"github.com/caimlas/meept/e2e/harness"
)

// insecureTLS returns the transport config for talking to the scratch
// daemon's self-signed TLS listener.
func insecureTLS() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // self-signed scratch cert
}

// start wraps harness.Start for local naming.
func start(t *testing.T) *harness.Stack {
	t.Helper()
	return harness.Start(t)
}
