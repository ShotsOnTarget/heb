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
		fmt.Fprintln(os.Stderr, "  install  write claude commands to .claude/commands/")
		fmt.Fprintln(os.Stderr, "  update   overwrite claude commands in .claude/commands/")
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

func runClaudeInstall(overwrite bool) int {
	dir, err := claudeCommandsDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb claude: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "heb claude: %v\n", err)
		return 1
	}

	entries, err := fs.ReadDir(commands.Files, ".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb claude: %v\n", err)
		return 1
	}

	var installed, skipped, updated int
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

		dest := filepath.Join(dir, name)
		existing, existErr := os.ReadFile(dest)

		if existErr == nil {
			// File exists
			if !overwrite {
				skipped++
				continue
			}
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

	if updated > 0 {
		fmt.Fprintf(os.Stderr, "updated %d, installed %d, skipped %d\n", updated, installed, skipped)
	} else if installed > 0 {
		fmt.Fprintf(os.Stderr, "installed %d commands, skipped %d\n", installed, skipped)
	} else {
		fmt.Fprintln(os.Stderr, "all commands already installed")
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

		dest := filepath.Join(dir, name)
		existing, existErr := os.ReadFile(dest)

		if existErr != nil {
			fmt.Printf("  %-20s missing\n", name)
		} else if sha256.Sum256(existing) == sha256.Sum256(embedded) {
			fmt.Printf("  %-20s ok\n", name)
		} else {
			fmt.Printf("  %-20s outdated\n", name)
		}
	}
	return 0
}
