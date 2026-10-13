// Package vendorpkgsim simulates a vendored package: it must NEVER be
// indexed (skip rule).
package vendorpkgsim

// Hidden is a symbol that must not appear in the index.
func Hidden() int { return 1 }
