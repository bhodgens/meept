//go:build mage

package main

import (
	"fmt"
	"os/exec"
)

// Test runs all tests
func Test() error {
	fmt.Println("Running tests...")
	return runGo("test", "./...")
}

// TestVerbose runs all tests with verbose output
func TestVerbose() error {
	fmt.Println("Running tests (verbose)...")
	return runGo("test", "-v", "./...")
}

// TestRace runs tests with race detection
func TestRace() error {
	fmt.Println("Running tests (race detection)...")
	return runGo("test", "-race", "./...")
}

// TestCoverage runs tests with coverage report
func TestCoverage() error {
	fmt.Println("Running tests (coverage)...")
	if err := runGo("test", "-coverprofile=coverage.out", "./..."); err != nil {
		return err
	}
	fmt.Println("\nCoverage report:")
	return runGo("tool", "cover", "-func=coverage.out")
}

// TestHTMLCoverage runs tests and opens HTML coverage report
func TestHTMLCoverage() error {
	fmt.Println("Running tests and generating HTML coverage...")
	if err := runGo("test", "-coverprofile=coverage.out", "./..."); err != nil {
		return err
	}
	return runGo("tool", "cover", "-html=coverage.out")
}

// Bench runs benchmarks
func Bench() error {
	fmt.Println("Running benchmarks...")
	return runGo("test", "-bench=.", "-run=^$", "./...")
}

// BenchAll runs all benchmarks including those marked skip
func BenchAll() error {
	fmt.Println("Running all benchmarks...")
	return runGo("test", "-bench=.", "-benchmem", "-run=^$", "./...")
}

// Lint runs golangci-lint
func Lint() error {
	fmt.Println("Running golangci-lint...")
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		return fmt.Errorf("golangci-lint not found, install: brew install golangci-lint")
	}
	return runCmd("golangci-lint", "run", "./...")
}

// Gosec runs security scanner
func Gosec() error {
	fmt.Println("Running gosec (G201, G202)...")
	if _, err := exec.LookPath("gosec"); err != nil {
		return fmt.Errorf("gosec not found, install: go install github.com/securego/gosec/v2/cmd/gosec@latest")
	}
	return runCmd("gosec", "-include=G201,G202", "./...")
}

// Vet runs go vet
func Vet() error {
	fmt.Println("Running go vet...")
	return runGo("vet", "./...")
}

// Fmt runs gofmt
func Fmt() error {
	fmt.Println("Running gofmt...")
	return runGo("fmt", "./...")
}

// Tidy runs go mod tidy
func Tidy() error {
	fmt.Println("Running go mod tidy...")
	return runGo("mod", "tidy")
}

// Check runs all checks (tidy, vet, lint, test)
func Check() error {
	fmt.Println("Running all checks...")
	if err := Tidy(); err != nil {
		return err
	}
	if err := Vet(); err != nil {
		return err
	}
	if err := Lint(); err != nil {
		return err
	}
	if err := Test(); err != nil {
		return err
	}
	fmt.Println("✓ All checks passed")
	return nil
}
