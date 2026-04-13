package retrieve

import "strings"

// Config holds all tunable thresholds for the recall pipeline.
// Defaults match the Part A spec and the original recall.md behaviour.
type Config struct {
	TokenBudget     int    // default 300
	GitResults      int    // default 3
	GitCap          int    // default 10
	BeadsResults    int    // default 2
	MemoryLimit     int    // default 16 (RecallLimit)
	MinComponentLen int    // default 2
	GitNoiseCap     int    // default 10
	FileGlob        string // default "*.gd"
	NoExternal     bool   // default false
	Format         string // "both" | "human" | "json"
}

// DefaultConfig returns the spec-mandated defaults.
func DefaultConfig() Config {
	return Config{
		TokenBudget:     300,
		GitResults:      3,
		GitCap:          10,
		BeadsResults:    2,
		MemoryLimit:     16,
		MinComponentLen: 2,
		GitNoiseCap:     10,
		FileGlob:        "*.gd",
		NoExternal:      false,
		Format:          "both",
	}
}

// splitToken splits a compound token on "_" into component words.
// Preserves order. Single-component tokens return a length-1 slice.
func splitToken(token string) []string {
	return strings.Split(token, "_")
}

// filterByLength drops components shorter than minLen.
// Spec §4.2: single-character components cannot be searched meaningfully
// (grep -rl "I" . returns everything). Default minLen = 2.
func filterByLength(components []string, minLen int) []string {
	out := make([]string, 0, len(components))
	for _, c := range components {
		if len(c) >= minLen {
			out = append(out, c)
		}
	}
	return out
}

// countingLookup is the signature passed to selectWinner: given a component
// word, return the number of refs it would produce via literal lookup.
// This is injected so selectWinner can be tested without exec or git.
type countingLookup func(component string) int

// selectWinner implements spec §4.5 multi-component selection.
//
// Returns the index of the winning component, or -1 if decomposition yields
// nothing (all components have zero refs and are not recoverable from noise
// fallback — but by §4.5.2 a zero-count component CAN win if every
// component is zero, so the only -1 case is empty input).
//
// Rules (spec §4.5):
//   - Noise cap: components with count > noiseCap are ineligible, unless
//     all are noisy — then the noisy set competes.
//   - Strict-tie: winner = component with strictly smallest count.
//     Ties (exact equality) broken by smallest index (first-component bias).
//   - Zero-count: all-zero input lets the first index win (§4.5.2).
func selectWinner(components []string, lookup countingLookup, noiseCap int) (int, []int) {
	if len(components) == 0 {
		return -1, nil
	}

	counts := make([]int, len(components))
	for i, c := range components {
		counts[i] = lookup(c)
	}

	// Classify noisy.
	noisy := make([]bool, len(components))
	anyNotNoisy := false
	for i, c := range counts {
		if c > noiseCap {
			noisy[i] = true
		} else {
			anyNotNoisy = true
		}
	}

	// Build eligible index set.
	var eligible []int
	if anyNotNoisy {
		for i := range components {
			if !noisy[i] {
				eligible = append(eligible, i)
			}
		}
	} else {
		// All noisy — fallback: full set competes, same rules.
		for i := range components {
			eligible = append(eligible, i)
		}
	}

	if len(eligible) == 0 {
		return -1, counts
	}

	// Find strict minimum count across eligible.
	min := counts[eligible[0]]
	for _, i := range eligible[1:] {
		if counts[i] < min {
			min = counts[i]
		}
	}

	// Collect T = {i : counts[i] == min and i in eligible}.
	// Strict-tie rule (§4.5.4). First index wins.
	for _, i := range eligible {
		if counts[i] == min {
			return i, counts
		}
	}

	return -1, counts
}

// decompose is the full decomposition flow: split → length filter →
// multi-component selection. Returns the winning component string or ""
// if decomposition yields nothing.
func decompose(token string, lookup countingLookup, cfg Config) (string, []string, []int) {
	parts := splitToken(token)
	parts = filterByLength(parts, cfg.MinComponentLen)

	if len(parts) == 0 {
		return "", parts, nil
	}
	if len(parts) == 1 {
		return parts[0], parts, []int{lookup(parts[0])}
	}

	idx, counts := selectWinner(parts, lookup, cfg.GitNoiseCap)
	if idx < 0 {
		return "", parts, counts
	}
	return parts[idx], parts, counts
}
