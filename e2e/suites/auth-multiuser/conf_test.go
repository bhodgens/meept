//go:build e2e

package authmultiuser

import "crypto/tls"

// tlsConf is the shared InsecureSkipVerify transport TLS config for the
// scratch daemon's self-signed listener.
var tlsConf = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // self-signed scratch cert
