package retrieve

import (
	"bytes"
	"sort"
	"strconv"
	"strings"

	"github.com/steelboltgames/heb/internal/memory"
)

// GitRef is a single git log entry surfaced by the recall pipeline.
type GitRef struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Age     string `json:"age"`
}

// gitPass executes the full git log retrieval cascade for the given
// contract:sense>recall tokens and returns up to cfg.GitCap deduplicated refs
// ranked by BM25 relevance against ALL query tokens.
//
// Pipeline:
//  1. Gather candidates via IDF-sorted literal/decomposed lookups (existing)
//  2. BM25-rank commit messages against the full token set
//  3. Drop zero-score commits (no query token in message)
//  4. Return top GitCap results
func gitPass(tokens []string, runner Runner, cfg Config) []GitRef {
	if cfg.NoExternal {
		return nil
	}

	// Phase 1: gather candidate commits (existing pipeline).
	candidates := gitCandidates(tokens, runner, cfg)
	if len(candidates) == 0 {
		return nil
	}

	// Phase 2: BM25-rank commit messages against all query tokens.
	docs := make([]memory.Doc, len(candidates))
	for i, ref := range candidates {
		docs[i] = memory.Doc{
			Words:   memory.Tokenize(ref.Message),
			AgeDays: parseAgeDays(ref.Age),
		}
	}

	ranked := memory.BM25Rank(docs, tokens)

	out := make([]GitRef, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, candidates[r.Index])
		if len(out) >= cfg.GitCap {
			break
		}
	}
	return out
}

// gitCandidates gathers deduplicated candidate refs across all tokens
// using IDF-sorted literal/decomposed lookups. No ranking — just collection.
func gitCandidates(tokens []string, runner Runner, cfg Config) []GitRef {
	sorted := idfSort(tokens, runner, cfg)

	// Gather more candidates than GitCap to give BM25 a richer pool.
	candidateCap := cfg.GitCap * 3
	if candidateCap < 30 {
		candidateCap = 30
	}

	var all []GitRef
	seen := make(map[string]bool)

	for _, token := range sorted {
		if len(all) >= candidateCap {
			break
		}
		refs := lookupLiteral(token, runner, cfg)
		if len(refs) == 0 {
			refs = lookupDecomposed(token, runner, cfg)
		}
		for _, r := range refs {
			if seen[r.Hash] {
				continue
			}
			seen[r.Hash] = true
			all = append(all, r)
			if len(all) >= candidateCap {
				break
			}
		}
	}
	return all
}

// parseAgeDays converts a git relative date string like "2 days ago",
// "3 weeks ago", "1 year, 2 months ago" into approximate days.
// Returns 0 for unparseable strings (treats as brand new).
func parseAgeDays(age string) float64 {
	age = strings.TrimSpace(age)
	// Split on commas to handle "1 year, 2 months ago".
	parts := strings.Split(age, ",")
	var totalDays float64
	for _, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		// Expect: "<number> <unit> [ago]"
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue
		}
		unit := strings.TrimSuffix(fields[1], "s") // "days" → "day"
		switch unit {
		case "second", "sec":
			totalDays += n / 86400
		case "minute", "min":
			totalDays += n / 1440
		case "hour":
			totalDays += n / 24
		case "day":
			totalDays += n
		case "week":
			totalDays += n * 7
		case "month":
			totalDays += n * 30
		case "year":
			totalDays += n * 365
		}
	}
	return totalDays
}

// idfSort reorders tokens by file grep frequency (ascending). Tokens that
// match fewer files are more specific and processed first, mirroring BM25's
// IDF insight: rare terms carry more signal. Stable sort preserves original
// order among tokens with equal frequency.
func idfSort(tokens []string, runner Runner, cfg Config) []string {
	type ranked struct {
		token string
		count int
	}
	items := make([]ranked, len(tokens))
	for i, t := range tokens {
		items[i] = ranked{token: t, count: len(grepFiles(t, runner, cfg))}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].count < items[j].count
	})
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.token
	}
	return out
}

// lookupLiteral runs the L1 file grep + git log, falling back to L2
// message grep. Short-circuits on the first non-empty result.
func lookupLiteral(token string, runner Runner, cfg Config) []GitRef {
	// L1 — file grep, then git log on matched files.
	files := grepFiles(token, runner, cfg)
	if len(files) > 0 {
		if len(files) > 5 {
			files = files[:5]
		}
		refs := gitLogPaths(files, runner)
		if len(refs) > 0 {
			return refs
		}
	}
	// L2 — fallback message grep.
	return gitLogGrep(token, runner)
}

// lookupDecomposed runs the Part A spec decomposition algorithm and
// re-emits refs for the winning component. Per §5 the spec allows
// a fresh call (no caching required).
func lookupDecomposed(token string, runner Runner, cfg Config) []GitRef {
	// Counting lookup: returns number of refs a literal call would produce.
	lookup := func(component string) int {
		return len(lookupLiteral(component, runner, cfg))
	}
	winner, _, _ := decompose(token, lookup, cfg)
	if winner == "" {
		return nil
	}
	return lookupLiteral(winner, runner, cfg)
}

// grepFiles runs grep -rl <token> . --include=<fileGlob>.
// Returns matched file paths. Silent on error.
func grepFiles(token string, runner Runner, cfg Config) []string {
	stdout, _, err := runner.Run("grep", "-rl", token, ".", "--include="+cfg.FileGlob)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// gitLogPaths runs git log on a set of file paths and returns parsed refs.
// Uses null separators (spec §8.1) to avoid corruption from tabs in commit
// subjects.
func gitLogPaths(paths []string, runner Runner) []GitRef {
	args := []string{"log", "--format=%h%x00%s%x00%cr%x00", "-z", "-10", "--all", "--"}
	args = append(args, paths...)
	stdout, _, err := runner.Run("git", args...)
	if err != nil {
		return nil
	}
	return parseNullSeparated(stdout)
}

// gitLogGrep runs git log --grep=<token> and returns parsed refs.
func gitLogGrep(token string, runner Runner) []GitRef {
	stdout, _, err := runner.Run("git", "log", "--format=%h%x00%s%x00%cr%x00", "-z", "-10", "--all", "--grep="+token)
	if err != nil {
		return nil
	}
	return parseNullSeparated(stdout)
}

// parseNullSeparated parses git log output produced with
// --format=%h%x00%s%x00%cr%x00 -z. The record separator is %x00, three
// fields per record (hash, subject, age), with a trailing empty string
// from -z.
func parseNullSeparated(data []byte) []GitRef {
	if len(data) == 0 {
		return nil
	}
	// git log with -z AND a format ending in %x00 emits both separators,
	// producing empty strings between records. Filter them out so the
	// three-field grouping stays aligned.
	raw := bytes.Split(data, []byte{0})
	parts := make([][]byte, 0, len(raw))
	for _, p := range raw {
		if len(p) > 0 {
			parts = append(parts, p)
		}
	}
	var refs []GitRef
	for i := 0; i+2 < len(parts); i += 3 {
		refs = append(refs, GitRef{
			Hash:    string(parts[i]),
			Message: string(parts[i+1]),
			Age:     string(parts[i+2]),
		})
	}
	return refs
}
