// Package testdatainner simulates a testdata dir: it must NEVER be indexed
// (skip rule).
package testdatainner

// Hidden is a symbol that must not appear in the index.
func Hidden() int { return 2 }
