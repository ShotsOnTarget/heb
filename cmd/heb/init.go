package main

import (
	"fmt"
	"os"

	"github.com/steelboltgames/heb/internal/store"
)

func runInit(_ []string) int {
	root, err := store.RepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb init: %v\n", err)
		return 1
	}
	s, created, err := store.Init(root)
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
	return 0
}
