package memory

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

// Sep is the canonical tuple separator used throughout heb for display.
const Sep = "\u00b7"

// SessionEnergyBudget is the maximum total tokens (across all atoms)
// that a single learn session may write. Atoms are accepted in
// confidence-descending order until this budget is exhausted.
const SessionEnergyBudget = 120

// AtomTokenCap is the ideal maximum token count for a single atom.
// Atoms at or below this length pay no verbosity penalty. Atoms above
// this get their effective confidence scaled down proportionally,
// mechanically discouraging verbose atoms without relying on the LLM.
const AtomTokenCap = 12

// VerbosityCost returns a multiplier in (0, 1] that penalises atoms
// exceeding AtomTokenCap. At or below the cap → 1.0 (no penalty).
// Double the cap → 0.5. Triple → 0.33. Pure math, no LLM trust.
func VerbosityCost(tokenCount int) float64 {
	if tokenCount <= AtomTokenCap {
		return 1.0
	}
	return float64(AtomTokenCap) / float64(tokenCount)
}

// Atom is the atomic unit of memory — a weighted text pattern (cell assembly).
// No forced structure. The body is free-form text that the learn step produces
// and the retrieve step matches against via BM25.
type Atom struct {
	ID          string  `json:"id"`
	Body        string  `json:"body"`
	Weight      float64 `json:"weight"`
	Status      string  `json:"status"`
	TopicTokens string  `json:"topic_tokens,omitempty"`
	CreatedAt   int64   `json:"created_at"`
	UpdatedAt   int64   `json:"updated_at"`
}

// Scored is an atom with a recall score attached.
type Scored struct {
	Atom
	Score  float64 `json:"score"`
	Source string  `json:"source"` // "match" or "edge"
}

// Tokenize breaks a body into matchable word tokens. This is the ONE
// tokenizer used by both BM25 recall and edge co-activation.
//
// Splits on: space, underscore, hyphen, dot, slash, middle-dot (·).
// Lowercases everything. Drops single-character tokens as noise.
func Tokenize(s string) []string {
	s = strings.ToLower(s)
	s = strings.NewReplacer(
		"_", " ",
		"\u00b7", " ",
		".", " ",
		"/", " ",
		"-", " ",
	).Replace(s)
	var out []string
	for _, w := range strings.Fields(s) {
		if len(w) > 1 {
			out = append(out, w)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// TokenCount returns the number of matchable tokens in a body.
func TokenCount(s string) int {
	return len(Tokenize(s))
}

// ID returns the deterministic content-address for a body.
// Lowercased, trimmed, SHA1-hashed.
func ID(body string) string {
	normalized := strings.ToLower(strings.TrimSpace(body))
	sum := sha1.Sum([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// MatchesWord returns true if needle equals any token in hayTokens.
// Whole-word matching — "low" matches "low" but not "follow".
func MatchesWord(hayTokens []string, needle string) bool {
	for _, w := range hayTokens {
		if w == needle {
			return true
		}
	}
	return false
}
