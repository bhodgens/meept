// Command predid runs the predid analyzer as a standalone go-vet-style
// checker.
//
// Usage:
//
//	go run ./tools/analyzers/predid/ ./...
package main

import (
	"github.com/caimlas/meept/tools/analyzers/predid/predid"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(predid.Analyzer) }
