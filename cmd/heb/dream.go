package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/steelboltgames/heb/internal/store"
)

func runDream(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: heb dream <subcommand> [args]")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  seeds         select seed memories for structured dreaming")
		fmt.Fprintln(os.Stderr, "  random-pairs  select random undreamt unconnected pairs")
		return 2
	}

	switch args[0] {
	case "seeds":
		return runDreamSeeds(args[1:])
	case "random-pairs":
		return runDreamRandomPairs(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "heb dream: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runDreamSeeds(args []string) int {
	fs := flag.NewFlagSet("dream seeds", flag.ContinueOnError)
	limit := fs.Int("limit", 3, "number of seeds to return")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	s, err := store.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb dream seeds: %v\n", err)
		return 1
	}
	defer s.Close()

	seeds, err := store.DreamSeeds(s.DB(), *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb dream seeds: %v\n", err)
		return 1
	}

	type seedOut struct {
		ID     string  `json:"id"`
		Body   string  `json:"body"`
		Weight float64 `json:"weight"`
	}
	out := make([]seedOut, len(seeds))
	for i, m := range seeds {
		out[i] = seedOut{
			ID:     m.ID,
			Body:   m.Body,
			Weight: m.Weight,
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "heb dream seeds: encode: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "dream seeds: %d candidates\n", len(out))
	return 0
}

func runDreamRandomPairs(args []string) int {
	fs := flag.NewFlagSet("dream random-pairs", flag.ContinueOnError)
	limit := fs.Int("limit", 5, "number of pairs to return")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	s, err := store.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb dream random-pairs: %v\n", err)
		return 1
	}
	defer s.Close()

	pairs, err := store.DreamRandomPairs(s.DB(), *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb dream random-pairs: %v\n", err)
		return 1
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(pairs); err != nil {
		fmt.Fprintf(os.Stderr, "heb dream random-pairs: encode: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "dream random-pairs: %d candidates\n", len(pairs))
	return 0
}
