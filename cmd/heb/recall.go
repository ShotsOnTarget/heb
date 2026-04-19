package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

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
	memories, maxScore := resolveMemories(in.Tokens, cfg.MemoryLimit, in.Project)

	result := retrieve.Run(retrieve.Input{
		SessionID: in.SessionID,
		Project:   in.Project,
		Tokens:    in.Tokens,
	}, memories, retrieve.ExecRunner{}, cfg)
	result.MaxPossibleScore = maxScore

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

	fmt.Fprintf(os.Stderr, "recall: %d memories (%d/%d tok), %d git (%d/%d tok), %d beads\n",
		len(result.Memories), result.TokensUsed, result.TokenBudget,
		len(result.GitRefs), result.GitTokensUsed, result.GitTokenBudget,
		len(result.Beads))

	// Emit compact recall details to stderr for the GUI
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
	return 0
}

// resolveMemories opens the store and runs Recall, returning an empty
// slice on any failure. Recall is best-effort — a missing store is a
// valid cold-start state. Memories are scoped to the given project.
// Also returns the theoretical score ceiling for this query so callers
// can normalise Score → relevance band.
func resolveMemories(tokens []string, limit int, project string) ([]store.Scored, float64) {
	s, err := store.Open()
	if err != nil {
		return nil, 0
	}
	defer s.Close()
	mems, maxPossible, err := store.Recall(s.DB(), tokens, limit, project)
	if err != nil {
		return nil, 0
	}
	return mems, maxPossible
}
