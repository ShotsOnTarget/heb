package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/steelboltgames/heb/internal/store"
)

func runMemories(args []string) int {
	fs := flag.NewFlagSet("memories", flag.ContinueOnError)
	project := fs.String("project", "", "filter by project path; use '.' for the current repo; empty lists all")
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
