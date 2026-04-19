package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func runGUI(_ []string) int {
	// Find the electron app directory relative to the heb binary.
	// Look in: <heb-binary-dir>/../electron, or <cwd>/electron
	electronDir := findElectronDir()
	if electronDir == "" {
		fmt.Fprintln(os.Stderr, "heb gui: cannot find electron/ directory")
		fmt.Fprintln(os.Stderr, "expected at: <heb-binary-dir>/../electron or ./electron")
		return 1
	}

	// Pass the current working directory so the Electron app knows
	// which project to operate on.
	cwd, _ := os.Getwd()

	// Find npx to launch electron
	npx := "npx"
	if runtime.GOOS == "windows" {
		npx = "npx.cmd"
	}

	// Pass the full path to the current heb binary so Electron can find it
	// even if $GOPATH/bin isn't on Electron's PATH.
	hebBin, _ := os.Executable()
	if hebBin == "" {
		hebBin = "heb"
	}

	cmd := exec.Command(npx, "electron", ".")
	cmd.Dir = electronDir
	cmd.Env = append(os.Environ(), "HEB_PROJECT="+cwd, "HEB_BIN="+hebBin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Fprintf(os.Stderr, "launching heb gui (project: %s)\n", cwd)

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "heb gui: %v\n", err)
		return 1
	}
	return 0
}

func findElectronDir() string {
	// 1. Relative to the heb binary
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Join(filepath.Dir(exe), "..", "electron")
		if isElectronDir(dir) {
			return dir
		}
		// Also check same directory as binary
		dir = filepath.Join(filepath.Dir(exe), "electron")
		if isElectronDir(dir) {
			return dir
		}
	}

	// 2. Relative to CWD (development mode)
	cwd, err := os.Getwd()
	if err == nil {
		dir := filepath.Join(cwd, "electron")
		if isElectronDir(dir) {
			return dir
		}
	}

	return ""
}

func isElectronDir(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "main.js"))
	return err == nil && !info.IsDir()
}
