package retrieve

import (
	"testing"
	"time"

	"github.com/steelboltgames/heb/internal/store"
)

func TestFilterSuperseded_RemovesOlderConflictingMemory(t *testing.T) {
	now := time.Now().Unix()
	fiveDaysAgo := now - 5*86400
	oneDayAgo := now - 1*86400

	memories := []store.Scored{
		{
			Memory: store.Memory{
				ID: "aaa", Body: "ShopEncounter costs 4",
				Weight: 0.80, Status: "active",
				CreatedAt: fiveDaysAgo, UpdatedAt: fiveDaysAgo,
			},
			Score: 1.5, Source: "match",
		},
		{
			Memory: store.Memory{
				ID: "bbb", Body: "ShopEncounter costs 6",
				Weight: 0.75, Status: "active",
				CreatedAt: oneDayAgo, UpdatedAt: oneDayAgo,
			},
			Score: 1.4, Source: "match",
		},
		{
			Memory: store.Memory{
				ID: "ccc", Body: "CombatScreen syncs combat state",
				Weight: 0.60, Status: "active",
				CreatedAt: fiveDaysAgo, UpdatedAt: fiveDaysAgo,
			},
			Score: 0.8, Source: "match",
		},
	}

	// Simulate Reflect flagging the older memory as superseded
	reflectJSON := `{
		"status": "conflicts",
		"conflicts": [
			{
				"existing_tuple": "ShopEncounter costs 4",
				"existing_weight": 0.80,
				"conflict_type": "superseded",
				"superseded_by": "ShopEncounter costs 6",
				"confidence": 0.90,
				"action": "create_successor"
			}
		],
		"extensions": [],
		"prediction": {},
		"notes": "",
		"proceed": true
	}`

	// BEFORE: 3 memories
	if len(memories) != 3 {
		t.Fatalf("expected 3 memories before filter, got %d", len(memories))
	}

	// AFTER: superseded memory removed
	filtered := FilterSuperseded(memories, reflectJSON)

	if len(filtered) != 2 {
		t.Fatalf("expected 2 memories after filter, got %d", len(filtered))
	}

	// The newer ShopEncounter memory survives
	found := false
	for _, m := range filtered {
		if m.Body == "ShopEncounter costs 6" {
			found = true
		}
		if m.Body == "ShopEncounter costs 4" {
			t.Error("superseded memory 'ShopEncounter costs 4' should have been filtered out")
		}
	}
	if !found {
		t.Error("newer memory 'ShopEncounter costs 6' should survive filtering")
	}

	// Unrelated memory survives
	found = false
	for _, m := range filtered {
		if m.Body == "CombatScreen syncs combat state" {
			found = true
		}
	}
	if !found {
		t.Error("unrelated memory 'CombatScreen syncs combat state' should survive filtering")
	}
}

func TestFilterSuperseded_NoConflicts(t *testing.T) {
	memories := []store.Scored{
		{Memory: store.Memory{Body: "foo"}, Score: 1.0, Source: "match"},
		{Memory: store.Memory{Body: "bar"}, Score: 0.5, Source: "match"},
	}

	reflectJSON := `{"status": "confirms", "conflicts": [], "proceed": true}`
	filtered := FilterSuperseded(memories, reflectJSON)

	if len(filtered) != 2 {
		t.Fatalf("expected 2 memories (no conflicts), got %d", len(filtered))
	}
}

func TestFilterSuperseded_EmptyReflect(t *testing.T) {
	memories := []store.Scored{
		{Memory: store.Memory{Body: "foo"}, Score: 1.0, Source: "match"},
	}

	filtered := FilterSuperseded(memories, "")
	if len(filtered) != 1 {
		t.Fatalf("expected 1 memory (empty reflect), got %d", len(filtered))
	}
}

func TestFilterSuperseded_NonSupersededConflictsPassThrough(t *testing.T) {
	memories := []store.Scored{
		{Memory: store.Memory{Body: "max hp is 100"}, Score: 1.0, Source: "match"},
	}

	// explicit_update conflicts should NOT be filtered — they're prompt-vs-memory
	reflectJSON := `{
		"status": "conflicts",
		"conflicts": [
			{
				"existing_tuple": "max hp is 100",
				"conflict_type": "explicit_update",
				"new_value": "max hp is 150",
				"confidence": 0.85,
				"action": "create_successor"
			}
		],
		"proceed": true
	}`

	filtered := FilterSuperseded(memories, reflectJSON)
	if len(filtered) != 1 {
		t.Fatalf("explicit_update conflicts should not be filtered, got %d", len(filtered))
	}
}

func TestFilterSuperseded_CaseInsensitiveMatch(t *testing.T) {
	memories := []store.Scored{
		{Memory: store.Memory{Body: "ShopEncounter costs 4"}, Score: 1.0, Source: "match"},
		{Memory: store.Memory{Body: "ShopEncounter costs 6"}, Score: 0.9, Source: "match"},
	}

	reflectJSON := `{
		"conflicts": [
			{
				"existing_tuple": "shopencounter costs 4",
				"conflict_type": "superseded",
				"superseded_by": "ShopEncounter costs 6",
				"confidence": 0.90
			}
		]
	}`

	filtered := FilterSuperseded(memories, reflectJSON)
	if len(filtered) != 1 {
		t.Fatalf("expected case-insensitive match to filter, got %d", len(filtered))
	}
	if filtered[0].Body != "ShopEncounter costs 6" {
		t.Errorf("wrong memory survived: %s", filtered[0].Body)
	}
}

func TestFilterSuperseded_MultipleSuperseded(t *testing.T) {
	memories := []store.Scored{
		{Memory: store.Memory{Body: "ShopEncounter costs 4"}, Score: 1.0, Source: "match"},
		{Memory: store.Memory{Body: "ShopEncounter costs 6"}, Score: 0.9, Source: "match"},
		{Memory: store.Memory{Body: "elite card reward is 3"}, Score: 0.8, Source: "match"},
		{Memory: store.Memory{Body: "elite card reward is 5"}, Score: 0.7, Source: "match"},
	}

	reflectJSON := `{
		"conflicts": [
			{
				"existing_tuple": "ShopEncounter costs 4",
				"conflict_type": "superseded",
				"superseded_by": "ShopEncounter costs 6",
				"confidence": 0.90
			},
			{
				"existing_tuple": "elite card reward is 3",
				"conflict_type": "superseded",
				"superseded_by": "elite card reward is 5",
				"confidence": 0.90
			}
		]
	}`

	filtered := FilterSuperseded(memories, reflectJSON)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 memories after filtering 2 superseded, got %d", len(filtered))
	}
	for _, m := range filtered {
		if m.Body == "ShopEncounter costs 4" || m.Body == "elite card reward is 3" {
			t.Errorf("superseded memory should not survive: %s", m.Body)
		}
	}
}
