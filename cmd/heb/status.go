package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/steelboltgames/heb/internal/store"
)

func runStatus(_ []string) int {
	root, err := store.RepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb status: %v\n", err)
		return 1
	}
	s, err := store.Open(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb status: %v\n", err)
		return 1
	}
	defer s.Close()

	st, err := s.Stats()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb status: %v\n", err)
		return 1
	}

	out, _ := json.MarshalIndent(st, "", "  ")
	fmt.Fprintln(os.Stdout, string(out))

	last := "never"
	if st.LastActivity > 0 {
		last = time.Unix(st.LastActivity, 0).UTC().Format(time.RFC3339)
	}
	ds, _ := s.DreamStats()

	lastDream := "never"
	if ds.LastDream > 0 {
		lastDream = time.Unix(ds.LastDream, 0).UTC().Format(time.RFC3339)
	}

	fmt.Fprintf(os.Stderr, `HEB STATUS
──────────────────────────────
backend:        %s (schema v%d)
memories:       %d active / %d total
edges:          %d
events:         %d
episodes:       %d
last activity:  %s
──────────────────────────────
DREAM
  dream memories:     %d
  tentative edges:    %d
  last dream:         %s
──────────────────────────────
`, st.Backend, st.SchemaVersion, st.Active, st.Memories, st.Edges, st.Events, st.Episodes, last,
		ds.DreamMemories, ds.TentativeEdges, lastDream)
	return 0
}
