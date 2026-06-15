// Package errcls provides shared error classification helpers that replace
// ad-hoc strings.Contains(err.Error(), ...) checks across the codebase.
//
// # Convention
//
// All error classification (rate-limit detection, auth errors, parameter
// errors, network errors, retryability, JSON syntax errors, SQLite
// duplicate-column detection, service-already-installed checks) MUST go
// through this package. Do NOT write strings.Contains(err.Error(), "rate
// limit") in calling code.
//
// To add a new classifier:
//  1. Add a function Is<Category>(err error) bool in this package
//  2. Use errors.As / errors.Is for structured detection when the error has
//     a typed sentinel or struct (e.g. *json.SyntaxError, *llm.APIError)
//  3. For packages that cannot be imported directly (import cycles),
//     provide a Register<Category>Sentinels function and call it from init()
//  4. For third-party drivers that return untyped fmt.Errorf strings (e.g.
//     SQLite drivers, kardianos/service), a centralized case-insensitive
//     substring check in this package is acceptable — the point is that the
//     check lives in ONE place, not scattered across the codebase
//  5. Callers use errcls.Is<Category>(err) instead of substring checks
//
// For command risk classification (shell.go classifyRisk), the pattern is
// different: use quote-aware tokenization (splitOnUnquotedPipes) rather
// than strings.Split to correctly handle pipe characters inside quoted
// strings.
//
// All helpers return false on nil errors. They use errors.As / errors.Is so
// wrapping via fmt.Errorf("...: %w", err) is handled correctly.
//
// To avoid import cycles, errcls only depends on internal/llm (a leaf
// package) and the standard library. Higher-level packages that define
// sentinel errors (e.g. internal/services) register them via
// RegisterSentinels at init time.
package errcls
