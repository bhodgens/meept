// Package employee — api_sanitize.go contains regex patterns used by
// api_handlers.go to sanitize error messages before sending them to HTTP
// clients. The patterns mirror internal/comm/http/server.go's sanitization
// regexes but are duplicated here to avoid an import cycle (internal/comm/http
// imports internal/employee when wiring the handler).
package employee

import "regexp"

var (
	// absPathReAgent matches Unix absolute paths and Windows drive-letter paths.
	absPathReAgent = regexp.MustCompile(`(?:/[A-Za-z0-9._-]+)+(?:\.[A-Za-z0-9]+)?|[A-Za-z]:\\[A-Za-z0-9._\\-]+`)
	// goImportPathReAgent matches domain/pkg import paths like
	// "github.com/caimlas/meept/...".
	goImportPathReAgent = regexp.MustCompile(`[a-z0-9.-]+\.[a-z]{2,}/[A-Za-z0-9._/-]+`)
	// fileLineReAgent matches "file.go:42:" or "file.go:42:43:" prefixes.
	fileLineReAgent = regexp.MustCompile(`[A-Za-z0-9_-]+\.go:\d+(?::\d+)?:\s*`)
)
