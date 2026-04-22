package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/steelboltgames/heb/internal/store"
)

func runMemories(args []string) int {
	fs := flag.NewFlagSet("memories", flag.ContinueOnError)
	project := fs.String("project", "", "filter by project path or registered name; use '.' for the current repo; empty lists all")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	s, err := store.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb memories: %v\n", err)
		return 1
	}
	defer s.Close()

	filter := *project
	if filter == "." {
		p, err := store.ProjectID()
		if err != nil {
			fmt.Fprintf(os.Stderr, "heb memories: resolve current project: %v\n", err)
			return 1
		}
		filter = p
	} else if filter != "" && !strings.ContainsAny(filter, "/\\") {
		// Looks like a bare name — try to resolve against registered projects.
		resolved, err := resolveProjectName(s.DB(), filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "heb memories: %v\n", err)
			return 1
		}
		filter = resolved
	}

	mems, err := store.ListMemories(s.DB(), filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb memories: %v\n", err)
		return 1
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(mems); err != nil {
		fmt.Fprintf(os.Stderr, "heb memories: encode: %v\n", err)
		return 1
	}

	scope := "all projects"
	if filter != "" {
		scope = filter
	}
	fmt.Fprintf(os.Stderr, "memories: %d active (%s)\n", len(mems), scope)
	return 0
}

// resolveProjectName maps a bare project name (e.g. "heb") to its registered
// repo path. Returns the original name unchanged if no project matches, so
// unrecognised values still flow through to the path-based query (yielding 0
// results, which is a clearer signal than silent name-vs-path confusion).
// Errors only when the name is ambiguous across multiple registered projects.
func resolveProjectName(db *sql.DB, name string) (string, error) {
	projects, err := store.ListProjects(db)
	if err != nil {
		return name, nil
	}
	var matches []string
	for _, p := range projects {
		if p.Name == name {
			matches = append(matches, p.Path)
		}
	}
	switch len(matches) {
	case 0:
		return name, nil
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("project name %q is ambiguous (matches %d registered projects); pass a path instead", name, len(matches))
	}
}
