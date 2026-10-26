package security

import "errors"

// ErrCommandBlocked is the sentinel for shell commands rejected by a
// security guard (path fence, risk classifier, permission table, or
// scanner) BEFORE execution. Transport surfaces map it to an
// authorization-shaped response (HTTP 403) instead of a server fault
// (500): a guard block is the security layer working as designed, not a
// daemon failure. The PTY endpoint already answers fence rejections with
// 403 (pty_handler.go); terminal/exec must not diverge from it.
var ErrCommandBlocked = errors.New("command blocked by security guard")
