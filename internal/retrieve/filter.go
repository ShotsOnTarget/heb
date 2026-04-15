package retrieve

import (
	"encoding/json"
	"strings"

	"github.com/steelboltgames/heb/internal/store"
)

// reflectConflict is the subset of a Reflect conflict entry we need for filtering.
type reflectConflict struct {
	ExistingTuple string `json:"existing_tuple"`
	ConflictType  string `json:"conflict_type"`
	SupersededBy  string `json:"superseded_by"`
}

// FilterSuperseded removes memories that Reflect flagged as superseded.
// It parses the conflicts array from reflectJSON, collects tuples with
// conflict_type "superseded", and drops matching memories from the list.
// Non-superseded memories pass through unchanged.
func FilterSuperseded(memories []store.Scored, reflectJSON string) []store.Scored {
	if reflectJSON == "" {
		return memories
	}

	var parsed struct {
		Conflicts []reflectConflict `json:"conflicts"`
	}
	if err := json.Unmarshal([]byte(reflectJSON), &parsed); err != nil {
		return memories
	}

	superseded := make(map[string]bool)
	for _, c := range parsed.Conflicts {
		if c.ConflictType == "superseded" {
			superseded[normalize(c.ExistingTuple)] = true
		}
	}
	if len(superseded) == 0 {
		return memories
	}

	filtered := make([]store.Scored, 0, len(memories))
	for _, m := range memories {
		if !superseded[normalize(m.Body)] {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
