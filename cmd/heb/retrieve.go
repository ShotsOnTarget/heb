package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/steelboltgames/heb/internal/retrieve"
	"github.com/steelboltgames/heb/internal/store"
)

// doRetrieve runs the retrieve step: queries memory graph, git, and beads
// for context matching the sense tokens. Returns the result and its JSON.
func doRetrieve(sense *senseResult) (*retrieve.Result, string, error) {
	cfg := retrieve.DefaultConfig()

	// Normalize: split compound tokens (e.g. "format_flag" → "format", "flag")
	// and strip leading dashes so retrieval gets maximum substring coverage.
	tokens := splitTokens(sense.Tokens)

	memories := resolveMemories(tokens, cfg.MemoryLimit)

	result := retrieve.Run(retrieve.Input{
		SessionID: sense.SessionID,
		Project:   sense.Project,
		Tokens:    tokens,
	}, memories, retrieve.ExecRunner{}, cfg)

	// Display
	fmt.Fprintln(os.Stderr, retrieve.RenderHuman(result))

	// Persist to session (best-effort)
	jsonOut := retrieve.RenderJSON(result)
	root, err := store.RepoRoot()
	if err == nil {
		s, err := store.Open(root)
		if err == nil {
			defer s.Close()
			if err := store.WriteContract(s.DB(), sense.SessionID, "recall", jsonOut); err != nil {
				fmt.Fprintf(os.Stderr, "heb: session write recall: %v\n", err)
			}
		}
	}

	fmt.Fprintf(os.Stderr, "recall: %d memories, %d git, %d beads, %d tokens used\n",
		len(result.Memories), len(result.GitRefs), len(result.Beads), result.TokensUsed)

	return result, jsonOut, nil
}

// splitTokens normalizes LLM-produced tokens for retrieval. It splits
// compound tokens on underscores (format_flag → format, flag), strips
// leading dashes (--format → format), and deduplicates.
func splitTokens(raw []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, t := range raw {
		t = strings.TrimLeft(t, "-")
		parts := strings.Split(t, "_")
		for _, p := range parts {
			p = strings.ToLower(strings.TrimSpace(p))
			if p != "" && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// runRetrieve is the `heb retrieve` entry point.
// Accepts a prompt (runs sense first) or piped sense JSON on stdin.
func runRetrieve(args []string) int {
	prompt := strings.Join(args, " ")
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "usage: heb retrieve <prompt>")
		return 2
	}

	sense, _, err := doSense(prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: %v\n", err)
		return 1
	}

	_, jsonOut, err := doRetrieve(sense)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: %v\n", err)
		return 1
	}

	fmt.Fprint(os.Stdout, jsonOut)
	return 0
}
