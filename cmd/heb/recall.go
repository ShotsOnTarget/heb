package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/steelboltgames/heb/internal/retrieve"
	"github.com/steelboltgames/heb/internal/store"
)

// recallInput accepts contract:sense>recall output shape.
type recallInput struct {
	SessionID string   `json:"session_id,omitempty"`
	Project   string   `json:"project,omitempty"`
	Tokens    []string `json:"tokens"`
}

func runRecall(args []string) int {
	cfg := retrieve.DefaultConfig()
	fs := flag.NewFlagSet("recall", flag.ContinueOnError)
	fs.IntVar(&cfg.TokenBudget, "token-budget", cfg.TokenBudget, "total output budget in tokens (~4 chars/token)")
	fs.IntVar(&cfg.GitResults, "git-results", cfg.GitResults, "max git refs after trimming")
	fs.IntVar(&cfg.GitCap, "git-cap", cfg.GitCap, "max git refs before trimming")
	fs.IntVar(&cfg.BeadsResults, "beads-results", cfg.BeadsResults, "max beads results after trimming")
	fs.IntVar(&cfg.MemoryLimit, "memory-limit", cfg.MemoryLimit, "memory store recall limit")
	fs.IntVar(&cfg.MinComponentLen, "min-component-len", cfg.MinComponentLen, "decomposition minimum component length")
	fs.IntVar(&cfg.GitNoiseCap, "git-noise-cap", cfg.GitNoiseCap, "decomposition noise cap")
	fs.StringVar(&cfg.FileGlob, "file-glob", cfg.FileGlob, "file grep pattern for decomposition")
	fs.BoolVar(&cfg.NoExternal, "no-external", cfg.NoExternal, "skip git/beads entirely, memories only")
	fs.StringVar(&cfg.Format, "format", cfg.Format, "output format: both | human | json")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb recall: read stdin: %v\n", err)
		return 1
	}
	var in recallInput
	if len(data) > 0 {
		if err := json.Unmarshal(data, &in); err != nil {
			fmt.Fprintf(os.Stderr, "heb recall: parse json: %v\n", err)
			return 1
		}
	}

	// Resolve memories from the store (best-effort — empty on any failure
	// so recall still works in cold start / non-repo contexts).
	memories := resolveMemories(in.Tokens, cfg.MemoryLimit)

	result := retrieve.Run(retrieve.Input{
		SessionID: in.SessionID,
		Project:   in.Project,
		Tokens:    in.Tokens,
	}, memories, retrieve.ExecRunner{}, cfg)

	switch cfg.Format {
	case "human":
		fmt.Fprint(os.Stdout, retrieve.RenderHuman(result))
	case "json":
		fmt.Fprint(os.Stdout, retrieve.RenderJSON(result))
	default: // "both"
		fmt.Fprint(os.Stdout, retrieve.RenderHuman(result))
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, retrieve.RenderJSON(result))
	}

	fmt.Fprintf(os.Stderr, "recall: %d memories, %d git, %d beads, %d tokens used\n",
		len(result.Memories), len(result.GitRefs), len(result.Beads), result.TokensUsed)
	return 0
}

// resolveMemories opens the store and runs Recall, returning an empty
// slice on any failure. Recall is best-effort — a missing store is a
// valid cold-start state.
func resolveMemories(tokens []string, limit int) []store.Scored {
	root, err := store.RepoRoot()
	if err != nil {
		return nil
	}
	s, err := store.Open(root)
	if err != nil {
		return nil
	}
	defer s.Close()
	mems, err := store.Recall(s.DB(), tokens, limit)
	if err != nil {
		return nil
	}
	return mems
}
