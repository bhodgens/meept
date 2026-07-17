package main

import (
	"github.com/caimlas/meept/tools/analyzers/selflock/selflock"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	singlechecker.Main(selflock.Analyzer)
}
