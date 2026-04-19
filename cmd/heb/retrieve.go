package main

import (
	"fmt"
	"os"
	"strings"
	"time"

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

	memories, maxScore := resolveMemories(tokens, cfg.MemoryLimit, sense.Project)

	result := retrieve.Run(retrieve.Input{
		SessionID: sense.SessionID,
		Project:   sense.Project,
		Tokens:    tokens,
	}, memories, retrieve.ExecRunner{}, cfg)
	result.MaxPossibleScore = maxScore

	// Emit summary + detail lines to stderr for the GUI
	fmt.Fprintf(os.Stderr, "recall: %d memories (%d/%d tok), %d git (%d/%d tok), %d beads\n",
		len(result.Memories), result.TokensUsed, result.TokenBudget,
		len(result.GitRefs), result.GitTokensUsed, result.GitTokenBudget,
		len(result.Beads))

	now := time.Now().Unix()
	for _, m := range result.Memories {
		age := int((now - m.UpdatedAt) / 86400)
		if age < 0 {
			age = 0
		}
		tag := "match"
		if m.Source == "edge" {
			tag = "edge"
		}
		fmt.Fprintf(os.Stderr, "recall-mem: [%s %.2f] %s +%.2f (%dd)\n", tag, m.Score, m.Body, m.Weight, age)
	}
	for _, g := range result.GitRefs {
		fmt.Fprintf(os.Stderr, "recall-git: [%.2f] %s %s (%dd)\n", g.Score, g.Hash, g.Message, int(g.AgeDays))
	}

	// Persist to session (best-effort)
	jsonOut := retrieve.RenderJSON(result)
	{
		s, err := store.Open()
		if err == nil {
			defer s.Close()
			if err := store.WriteContract(s.DB(), sense.SessionID, "recall", jsonOut); err != nil {
				fmt.Fprintf(os.Stderr, "heb: session write recall: %v\n", err)
			}
		}
	}

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
