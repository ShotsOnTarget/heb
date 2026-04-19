package memory

import "math"

// BM25 tuning constants.
const (
	BM25K1 = 1.2  // term frequency saturation
	BM25B  = 0.75 // document length normalization
)

// RecencyHalfLifeDays controls how fast the temporal boost decays.
// At this age the recency factor is 0.5; at 2× it is ~0.33.
const RecencyHalfLifeDays = 30.0

// RecencyInfluence is the maximum ±swing from the recency factor.
// 0.15 means a brand-new document scores up to +15% over an ancient one.
const RecencyInfluence = 0.15

// Doc is a tokenized document with optional metadata for scoring.
type Doc struct {
	Words   []string // tokenized content
	Weight  float64  // confidence weight (0-1); 0 means no weight influence
	AgeDays float64  // age in days for recency decay; negative clamped to 0
}

// Scored is a BM25 score paired with the original index.
type BM25Scored struct {
	Index int
	Score float64
}

// RecencyFactor returns the recency multiplier for a given age in days.
// Brand new → 1.0, at half-life → 0.5, tapering toward 0.
// The returned band is (1-influence) to 1.0.
func RecencyFactor(ageDays float64) float64 {
	if ageDays < 0 {
		ageDays = 0
	}
	recency := 1.0 / (1.0 + ageDays/RecencyHalfLifeDays)
	return (1 - RecencyInfluence) + RecencyInfluence*recency
}

// BM25Rank scores a set of documents against query tokens and returns
// scored results sorted descending by score. Documents with zero score
// (no query token match) are excluded.
//
// This is the shared scoring core used by both memory recall and git
// commit ranking. Weight and recency are mild tiebreakers — BM25
// relevance is the dominant signal.
//
// Scoring formula:
//
//	bm25 = Σ(IDF[i] × TF_component[i])
//	score = bm25 × (0.7 + 0.3×weight) × recencyFactor(ageDays)
//
// If weight is 0 for all docs, the weight factor is a constant 0.7.
func BM25Rank(docs []Doc, query []string) []BM25Scored {
	if len(docs) == 0 || len(query) == 0 {
		return nil
	}

	// Tokenize and deduplicate query terms. Compound identifiers like
	// "PlayerController" split into ["player", "controller"] so they
	// match tokenized document content.
	seen := make(map[string]bool)
	var normalized []string
	for _, t := range query {
		for _, w := range Tokenize(t) {
			if !seen[w] {
				seen[w] = true
				normalized = append(normalized, w)
			}
		}
	}
	if len(normalized) == 0 {
		return nil
	}

	// Corpus stats across ALL documents.
	N := float64(len(docs))
	var totalWords int
	for _, d := range docs {
		totalWords += len(d.Words)
	}
	avgDL := float64(totalWords) / N

	// Document frequency per query token.
	docFreq := make([]int, len(normalized))
	for _, d := range docs {
		for i, qt := range normalized {
			if MatchesWord(d.Words, qt) {
				docFreq[i]++
			}
		}
	}

	// IDF per query token.
	idf := make([]float64, len(normalized))
	for i, df := range docFreq {
		n := float64(df)
		idf[i] = math.Log((N - n + 0.5) / (n + 0.5) + 1)
	}

	// Score each document.
	var results []BM25Scored
	for idx, d := range docs {
		dl := float64(len(d.Words))
		var bm25 float64
		for i, qt := range normalized {
			if !MatchesWord(d.Words, qt) {
				continue
			}
			tf := 0
			for _, w := range d.Words {
				if w == qt {
					tf++
				}
			}
			tfComp := (float64(tf) * (BM25K1 + 1)) / (float64(tf) + BM25K1*(1-BM25B+BM25B*dl/avgDL))
			bm25 += idf[i] * tfComp
		}
		if bm25 <= 0 {
			continue
		}

		// Weight as mild tiebreaker: ±30% influence.
		weightFactor := 0.7 + 0.3*d.Weight

		// Recency as mild tiebreaker: ±15% influence.
		recencyFactor := RecencyFactor(d.AgeDays)

		score := bm25 * weightFactor * recencyFactor
		results = append(results, BM25Scored{Index: idx, Score: score})
	}

	// Sort descending by score.
	sortBM25Scored(results)
	return results
}

// BM25MaxPossible returns the theoretical ceiling that BM25Rank's score
// could reach for the given query against the corpus — Σ IDF[i] × (K1+1),
// skipping tokens with zero document frequency (they can't contribute to
// any real score, so they'd only inflate the ceiling). Used to normalise
// returned scores into a stable range regardless of query absolute scale.
// Weight and recency factors both cap at 1.0, so BM25 ceiling == score ceiling.
func BM25MaxPossible(docs []Doc, query []string) float64 {
	if len(docs) == 0 || len(query) == 0 {
		return 0
	}
	seen := make(map[string]bool)
	var normalized []string
	for _, t := range query {
		for _, w := range Tokenize(t) {
			if !seen[w] {
				seen[w] = true
				normalized = append(normalized, w)
			}
		}
	}
	if len(normalized) == 0 {
		return 0
	}
	N := float64(len(docs))
	var max float64
	for _, qt := range normalized {
		var df int
		for _, d := range docs {
			if MatchesWord(d.Words, qt) {
				df++
			}
		}
		if df == 0 {
			continue
		}
		n := float64(df)
		idf := math.Log((N-n+0.5)/(n+0.5) + 1)
		max += idf * (BM25K1 + 1)
	}
	return max
}

func sortBM25Scored(s []BM25Scored) {
	// Simple insertion sort — corpus sizes are small (≤16 memories, ≤10 git refs).
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Score > s[j-1].Score; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func lower(s string) string {
	// Avoid importing strings for a single trivial helper.
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
