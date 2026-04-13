package main

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/steelboltgames/heb/commands"
)

func runClaude(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: heb claude <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  install  sync claude commands to .claude/commands/")
		fmt.Fprintln(os.Stderr, "  update   alias for install")
		fmt.Fprintln(os.Stderr, "  status   show installed vs embedded command status")
		return 2
	}

	switch args[0] {
	case "install":
		return runClaudeInstall(false)
	case "update":
		return runClaudeInstall(true)
	case "status":
		return runClaudeStatus()
	default:
		fmt.Fprintf(os.Stderr, "heb claude: unknown subcommand %q\n", args[0])
		return 2
	}
}

func claudeCommandsDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, ".claude", "commands"), nil
}

// hebSubDir is the namespaced subdirectory for heb commands.
// Files placed here become "heb:<name>" in Claude Code.
const hebSubDir = "heb"

// isRootCommand returns true for commands that should stay at
// .claude/commands/ root (not namespaced).
func isRootCommand(name string) bool {
	return name == "heb.md"
}

func runClaudeInstall(_ bool) int {
	dir, err := claudeCommandsDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb claude: %v\n", err)
		return 1
	}

	subDir := filepath.Join(dir, hebSubDir)
	if err := os.MkdirAll(subDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "heb claude: %v\n", err)
		return 1
	}

	entries, err := fs.ReadDir(commands.Files, ".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb claude: %v\n", err)
		return 1
	}

	// Track embedded filenames for stale cleanup.
	embeddedNames := make(map[string]bool)

	var installed, skipped, updated int
	for _, entry := range entries {
		name := entry.Name()
		if filepath.Ext(name) != ".md" {
			continue
		}
		embeddedNames[name] = true

		embedded, err := fs.ReadFile(commands.Files, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "heb claude: read embedded %s: %v\n", name, err)
			return 1
		}

		// Root commands stay in .claude/commands/, others go in heb/ subdir.
		var dest string
		if isRootCommand(name) {
			dest = filepath.Join(dir, name)
		} else {
			dest = filepath.Join(subDir, name)
		}
		existing, existErr := os.ReadFile(dest)

		if existErr == nil {
			// File exists — always sync to latest embedded.
			if sha256.Sum256(existing) == sha256.Sum256(embedded) {
				skipped++
				continue
			}
			updated++
		} else {
			installed++
		}

		if err := os.WriteFile(dest, embedded, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "heb claude: write %s: %v\n", name, err)
			return 1
		}
	}

	// Remove stale commands from heb/ subdir that are no longer embedded.
	var removed int
	staleEntries, _ := os.ReadDir(subDir)
	for _, entry := range staleEntries {
		name := entry.Name()
		if filepath.Ext(name) != ".md" {
			continue
		}
		if !embeddedNames[name] && !isRootCommand(name) {
			if err := os.Remove(filepath.Join(subDir, name)); err == nil {
				removed++
			}
		}
	}

	switch {
	case updated > 0 || removed > 0:
		fmt.Fprintf(os.Stderr, "updated %d, installed %d, removed %d, unchanged %d\n", updated, installed, removed, skipped)
	case installed > 0:
		fmt.Fprintf(os.Stderr, "installed %d commands, unchanged %d\n", installed, skipped)
	default:
		fmt.Fprintln(os.Stderr, "all commands up to date")
	}

	return 0
}

func runClaudeStatus() int {
	dir, err := claudeCommandsDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb claude: %v\n", err)
		return 1
	}

	entries, err := fs.ReadDir(commands.Files, ".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb claude: %v\n", err)
		return 1
	}

	for _, entry := range entries {
		name := entry.Name()
		if filepath.Ext(name) != ".md" {
			continue
		}

		embedded, err := fs.ReadFile(commands.Files, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "heb claude: read embedded %s: %v\n", name, err)
			return 1
		}

		var dest string
		var displayName string
		if isRootCommand(name) {
			dest = filepath.Join(dir, name)
			displayName = name
		} else {
			dest = filepath.Join(dir, hebSubDir, name)
			displayName = hebSubDir + "/" + name
		}
		existing, existErr := os.ReadFile(dest)

		if existErr != nil {
			fmt.Printf("  %-25s missing\n", displayName)
		} else if sha256.Sum256(existing) == sha256.Sum256(embedded) {
			fmt.Printf("  %-25s ok\n", displayName)
		} else {
			fmt.Printf("  %-25s outdated\n", displayName)
		}
	}
	return 0
}
