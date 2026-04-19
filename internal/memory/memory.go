package memory

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"

	"github.com/kljensen/snowball/english"
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
// Splits on: camelCase boundaries, digit/letter transitions, and any
// non-alphanumeric character. Lowercases everything. Drops single-
// character tokens as noise. Applies Porter2 (Snowball English) stemming
// to alphabetic tokens so morphological variants match symmetrically at
// write and read time (e.g. "interaction" and "interactions" collapse
// to the same stem). Pure-digit tokens pass through unchanged.
func Tokenize(s string) []string {
	// Split camelCase and digit/letter boundaries before lowercasing.
	s = splitIdentifier(s)
	s = strings.ToLower(s)
	// Replace any non-alphanumeric char with space.
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isAlphaNum(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	var out []string
	for _, w := range strings.Fields(b.String()) {
		if len(w) <= 1 {
			continue
		}
		if w[0] >= 'a' && w[0] <= 'z' {
			w = english.Stem(w, true)
		}
		out = append(out, w)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// splitIdentifier inserts spaces at word boundaries within code identifiers:
// camelCase, PascalCase, digit↔letter transitions.
//
//	"CombatScreen"      → "Combat Screen"
//	"getHTTPResponse"   → "get HTTP Response"
//	"vec3"              → "vec 3"
//	"player2controller" → "player 2 controller"
//	"int32"             → "int 32"
func splitIdentifier(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	runes := []rune(s)
	for i, r := range runes {
		if i > 0 {
			prev := runes[i-1]
			// Letter case boundaries (camelCase).
			if isUpper(r) && isLower(prev) {
				b.WriteByte(' ')
			} else if isUpper(r) && isUpper(prev) && i+1 < len(runes) && isLower(runes[i+1]) {
				b.WriteByte(' ')
			}
			// Digit ↔ letter transitions.
			if isLetter(r) && isDigit(prev) {
				b.WriteByte(' ')
			} else if isDigit(r) && isLetter(prev) {
				b.WriteByte(' ')
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isUpper(r rune) bool   { return r >= 'A' && r <= 'Z' }
func isLower(r rune) bool   { return r >= 'a' && r <= 'z' }
func isLetter(r rune) bool  { return isUpper(r) || isLower(r) }
func isDigit(r rune) bool   { return r >= '0' && r <= '9' }
func isAlphaNum(r rune) bool { return isLetter(r) || isDigit(r) }

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
