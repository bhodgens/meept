//go:build mage

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Build builds all binaries (daemon + CLI + tools)
func Build() error {
	if err := BuildDaemon(); err != nil {
		return err
	}
	if err := BuildCLI(); err != nil {
		return err
	}
	if err := BuildGendoc(); err != nil {
		return err
	}
	if err := BuildGendocOpenAPI(); err != nil {
		return err
	}
	fmt.Println("✓ All binaries built successfully")
	return nil
}

// BuildDaemon builds the meept-daemon binary
func BuildDaemon() error {
	fmt.Println("Building meept-daemon...")
	return goBuild("./cmd/meept-daemon", "bin/meept-daemon")
}

// BuildCLI builds the meept CLI binary
func BuildCLI() error {
	fmt.Println("Building meept CLI...")
	return goBuild("./cmd/meept", "bin/meept")
}

// BuildGendoc builds the gendoc documentation generator
func BuildGendoc() error {
	fmt.Println("Building gendoc...")
	return goBuild("./cmd/gendoc", "bin/gendoc")
}

// BuildGendocOpenAPI builds the OpenAPI documentation generator
func BuildGendocOpenAPI() error {
	fmt.Println("Building gendoc-openapi...")
	return goBuild("./cmd/gendoc-openapi", "bin/gendoc-openapi")
}

// BuildLite builds the lite TUI client
func BuildLite() error {
	fmt.Println("Building meept-lite...")
	return goBuild("./cmd/meept-lite", "bin/meept-lite")
}

// BuildRelease builds all binaries with release optimizations
func BuildRelease() error {
	fmt.Println("Building release binaries...")
	ldflags := "-s -w"
	return runGo("build", "-ldflags", ldflags, "./...")
}

func goBuild(pkg, out string) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	return runGo("build", "-o", out, pkg)
}
