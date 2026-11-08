// Package telnyx is the Telnyx messaging (SMS) client (contact-channels
// plan, contract C5, owner leaf 12). The webhook receiver half lives in
// internal/comm/http and reuses the existing Ed25519 telnyx-signature
// verifier - never a second one.
//
// GATE PLACEHOLDER (00T-gate-tests.md): this file exists ONLY so the
// package directory has a non-test Go file and `go build ./...` stays
// green while the RED unit test (messaging_test.go) fails on undefined
// symbols. It declares no symbols and will be replaced by the leaf-12
// implementation (messaging.go). No implementation code lives here.
package telnyx
