// Command fieldguard runs the fieldguard analyzer as a standalone go-vet-style
// checker.
//
// Usage:
//
//	go run ./tools/analyzers/fieldguard/ ./...
package main

import (
	"github.com/caimlas/meept/tools/analyzers/fieldguard/fieldguard"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(fieldguard.Analyzer) }
