// Command mutexio runs the mutexio analyzer as a standalone go-vet-style
// checker.
//
// Usage:
//
//	go run ./tools/analyzers/mutexio/ ./...
package main

import (
	"github.com/caimlas/meept/tools/analyzers/mutexio/mutexio"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(mutexio.Analyzer) }
