package retrieve

import (
	"bytes"
	"strings"
)

// GitRef is a single git log entry surfaced by the recall pipeline.
type GitRef struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Age     string `json:"age"`
}

// gitPass executes the full git log retrieval cascade for the given
// contract:sense>recall tokens and returns up to cfg.GitCap deduplicated refs
// across all tokens. Processing is in-order-with-early-stop (spec §7).
func gitPass(tokens []string, runner Runner, cfg Config) []GitRef {
	if cfg.NoExternal {
		return nil
	}

	var all []GitRef
	seen := make(map[string]bool)

	for _, token := range tokens {
		if len(all) >= cfg.GitCap {
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
			if len(all) >= cfg.GitCap {
				break
			}
		}
	}
	return all
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
