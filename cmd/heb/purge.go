package main

import (
	"fmt"
	"os"

	"github.com/steelboltgames/heb/internal/store"
)

func runPurge(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: heb purge <id> [id...]")
		fmt.Fprintln(os.Stderr, "Deletes memories by ID. Cascades to events, provenance, edges, and dream_pairs.")
		return 2
	}

	root, err := store.RepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb purge: %v\n", err)
		return 1
	}
	s, err := store.Open(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb purge: %v\n", err)
		return 1
	}
	defer s.Close()

	deleted, err := store.PurgeMemories(s.DB(), args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb purge: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "purged %d memories\n", deleted)
	return 0
}
