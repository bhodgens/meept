//go:build mage

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Gui builds both the menubar and Flutter UI targets.
func Gui() error {
	if err := Menubar(); err != nil {
		return err
	}
	if err := Flutter(); err != nil {
		return err
	}
	fmt.Println("All GUI targets built successfully")
	return nil
}

// Menubar builds the Swift menubar app via swift build.
func Menubar() error {
	if runtime.GOOS != "darwin" {
		fmt.Println("Skipping menubar build (not macOS)")
		return nil
	}

	fmt.Println("Building menubar app (SPM)...")
	cmd := exec.Command("swift", "build", "-c", "release")
	cmd.Dir = filepath.Join(moduleRoot(), "menubar")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("menubar build failed: %w", err)
	}
	fmt.Println("Built menubar app")
	return nil
}

// Flutter builds the Flutter UI for the current platform.
func Flutter() error {
	fmt.Println("Building Flutter UI...")

	flutterDir := filepath.Join(moduleRoot(), "ui", "flutter_ui")

	// Determine platform.
	platform := "macos"
	if runtime.GOOS != "darwin" {
		platform = "linux"
	}

	cmd := exec.Command("flutter", "build", platform, "--release")
	cmd.Dir = flutterDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("flutter build failed: %w", err)
	}
	fmt.Printf("Built Flutter UI (%s)\n", platform)
	return nil
}
