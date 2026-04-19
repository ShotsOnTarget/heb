package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func runBuild(_ []string) int {
	start := time.Now()

	// Step 1: Build the Go binary
	fmt.Fprintln(os.Stderr, "▸ building go binary...")
	goBuild := exec.Command("go", "build", "-o", "heb.exe", "./cmd/heb/")
	goBuild.Stdout = os.Stdout
	goBuild.Stderr = os.Stderr
	if err := goBuild.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "go build failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "  go binary ok")

	// Step 2: Install the binary
	fmt.Fprintln(os.Stderr, "▸ installing go binary...")
	goInstall := exec.Command("go", "install", "./cmd/heb/")
	goInstall.Stdout = os.Stdout
	goInstall.Stderr = os.Stderr
	if err := goInstall.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "go install failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "  go install ok")

	// Step 3: Build the Electron renderer
	electronDir := findElectronDir()
	if electronDir == "" {
		fmt.Fprintln(os.Stderr, "▸ skipping electron build (electron/ not found)")
	} else {
		fmt.Fprintln(os.Stderr, "▸ building electron app...")
		npmBuild := exec.Command("npm", "run", "build")
		npmBuild.Dir = electronDir
		npmBuild.Stdout = os.Stdout
		npmBuild.Stderr = os.Stderr
		if err := npmBuild.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "electron build failed: %v\n", err)
			return 1
		}
		fmt.Fprintln(os.Stderr, "  electron build ok")
	}

	// Step 4: Clean up local build artifact
	os.Remove("heb.exe")

	elapsed := time.Since(start).Round(time.Millisecond)
	fmt.Fprintf(os.Stderr, "▸ build complete (%s)\n", elapsed)

	// Report installed binary location
	if p, err := exec.LookPath("heb"); err == nil {
		abs, _ := filepath.Abs(p)
		fmt.Fprintf(os.Stderr, "  binary: %s\n", abs)
	}

	return 0
}
