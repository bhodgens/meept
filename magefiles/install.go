//go:build mage

package main

import (
	"fmt"
	"os/exec"
)

// Install installs all binaries to $GOPATH/bin
func Install() error {
	fmt.Println("Installing meept binaries...")
	if err := installBin("./cmd/meept-daemon", "meept-daemon"); err != nil {
		return err
	}
	if err := installBin("./cmd/meept", "meept"); err != nil {
		return err
	}
	if err := installBin("./cmd/gendoc", "gendoc"); err != nil {
		return err
	}
	if err := installBin("./cmd/gendoc-openapi", "gendoc-openapi"); err != nil {
		return err
	}
	fmt.Println("✓ All binaries installed to $GOPATH/bin")
	return nil
}

// InstallDaemon installs only the daemon binary
func InstallDaemon() error {
	fmt.Println("Installing meept-daemon...")
	return installBin("./cmd/meept-daemon", "meept-daemon")
}

// InstallCLI installs only the CLI binary
func InstallCLI() error {
	fmt.Println("Installing meept CLI...")
	return installBin("./cmd/meept", "meept")
}

func installBin(pkg, name string) error {
	cmd := exec.Command("go", "install", pkg)
	cmd.Dir = moduleRoot()
	cmd.Stdout = nil // Suppress output for cleaner install
	cmd.Stderr = cmd.Stdout
	if err := cmd.Run(); err != nil {
		return err
	}
	fmt.Printf("  ✓ %s\n", name)
	return nil
}
