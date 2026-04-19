package main

import (
	"fmt"
	"os"
	"path"

	"github.com/steelboltgames/heb/internal/store"
)

func runInit(_ []string) int {
	s, created, err := store.Init()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb init: %v\n", err)
		return 1
	}
	defer s.Close()

	if created {
		fmt.Fprintf(os.Stderr, "initialised heb at %s\n", s.Path())
	} else {
		fmt.Fprintf(os.Stderr, "heb already initialised at %s\n", s.Path())
	}

	// Register the current project so it appears in `heb projects`
	projectPath, err := store.ProjectID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb init: project id: %v\n", err)
		return 0 // non-fatal — DB was created
	}
	name := path.Base(projectPath)
	if err := store.RegisterProject(s.DB(), projectPath, name); err != nil {
		fmt.Fprintf(os.Stderr, "heb init: register project: %v\n", err)
		return 0
	}
	fmt.Fprintf(os.Stderr, "registered project: %s (%s)\n", name, projectPath)

	return 0
}
