//go:build mage

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func runGo(args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = moduleRoot()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = moduleRoot()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func moduleRoot() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Dir(filepath.Dir(filename))
}
