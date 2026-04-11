package retrieve

import "testing"

// mockCounts returns a lookup function that reports canned counts keyed by
// component string. Components not in the map report 0.
func mockCounts(m map[string]int) countingLookup {
	return func(c string) int {
		return m[c]
	}
}

// TestDecompose covers all 13 worked examples in j67 Part A §6.
// Each case asserts the winning component (or "" for none).
// Examples 1, 2 do not exercise decompose — they resolve in the literal
// lookup cascade. Examples 10, 12 also resolve at the literal step.
// The remaining examples exercise decompose and are tested here.
func TestDecompose(t *testing.T) {
	cfg := DefaultConfig()

	cases := []struct {
		name   string
		token  string
		counts map[string]int
		want   string // winning component, "" if none
		wantParts []string
	}{
		// Example 3 — specificity wins, noise cap eliminates "stats"
		{
			name:   "ex3_drone_stats",
			token:  "drone_stats",
			counts: map[string]int{"drone": 8, "stats": 14},
			want:   "drone",
			wantParts: []string{"drone", "stats"},
		},
		// Example 4 — strict minimum, player wins at 5 over combat at 6
		{
			name:   "ex4_player_combat",
			token:  "player_combat",
			counts: map[string]int{"player": 5, "combat": 6},
			want:   "player",
			wantParts: []string{"player", "combat"},
		},
		// Example 5 — strict minimum wins over earlier components
		{
			name:   "ex5_enemy_spawn_system",
			token:  "enemy_spawn_system",
			counts: map[string]int{"enemy": 4, "spawn": 2, "system": 9},
			want:   "spawn",
			wantParts: []string{"enemy", "spawn", "system"},
		},
		// Example 6 — noise cap eliminates generic components
		{
			name:   "ex6_item_data_cache",
			token:  "item_data_cache",
			counts: map[string]int{"item": 34, "data": 41, "cache": 2},
			want:   "cache",
			wantParts: []string{"item", "data", "cache"},
		},
		// Example 7 — all components pass length >= 2, strict-tie picks map
		{
			name:   "ex7_ui_id_map",
			token:  "ui_id_map",
			counts: map[string]int{"ui": 8, "id": 5, "map": 3},
			want:   "map",
			wantParts: []string{"ui", "id", "map"},
		},
		// Example 8 — all zero counts, first-component bias picks flux
		{
			name:   "ex8_flux_capacitor",
			token:  "flux_capacitor",
			counts: map[string]int{"flux": 0, "capacitor": 0},
			want:   "flux",
			wantParts: []string{"flux", "capacitor"},
		},
		// Example 9 — all noisy, fallback, strict-tie picks state (30 < 42)
		{
			name:   "ex9_game_state",
			token:  "game_state",
			counts: map[string]int{"game": 42, "state": 30},
			want:   "state",
			wantParts: []string{"game", "state"},
		},
		// Example 11 — noise cap handles "the"
		{
			name:   "ex11_the_thing",
			token:  "the_thing",
			counts: map[string]int{"the": 47, "thing": 1},
			want:   "thing",
			wantParts: []string{"the", "thing"},
		},
		// Example 13 — no stopword filter, "my" wins by specificity
		{
			name:   "ex13_my_profile",
			token:  "my_profile",
			counts: map[string]int{"my": 3, "profile": 6},
			want:   "my",
			wantParts: []string{"my", "profile"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, parts, _ := decompose(tc.token, mockCounts(tc.counts), cfg)
			if got != tc.want {
				t.Errorf("winner = %q, want %q", got, tc.want)
			}
			if len(parts) != len(tc.wantParts) {
				t.Errorf("parts = %v, want %v", parts, tc.wantParts)
				return
			}
			for i, p := range parts {
				if p != tc.wantParts[i] {
					t.Errorf("parts[%d] = %q, want %q", i, p, tc.wantParts[i])
				}
			}
		})
	}
}

// TestDecomposeLengthFilter verifies the length filter drops single-char
// components, per spec §4.2 examples.
func TestDecomposeLengthFilter(t *testing.T) {
	cfg := DefaultConfig()

	cases := []struct {
		token string
		want  []string
	}{
		{"drone_I", []string{"drone"}}, // "I" < 2
		{"level_3", []string{"level"}}, // "3" < 2
		{"ui_map", []string{"ui", "map"}},
		{"my_profile", []string{"my", "profile"}},
	}

	for _, tc := range cases {
		t.Run(tc.token, func(t *testing.T) {
			parts := splitToken(tc.token)
			parts = filterByLength(parts, cfg.MinComponentLen)
			if len(parts) != len(tc.want) {
				t.Errorf("filter(%q) = %v, want %v", tc.token, parts, tc.want)
				return
			}
			for i, p := range parts {
				if p != tc.want[i] {
					t.Errorf("filter(%q)[%d] = %q, want %q", tc.token, i, p, tc.want[i])
				}
			}
		})
	}
}

// TestDecomposeEmptyAfterFilter verifies tokens with all components
// dropped by length filter yield no winner.
func TestDecomposeEmptyAfterFilter(t *testing.T) {
	cfg := DefaultConfig()
	got, parts, _ := decompose("a_b_c", mockCounts(nil), cfg)
	if got != "" {
		t.Errorf("winner for all-dropped token = %q, want empty", got)
	}
	if len(parts) != 0 {
		t.Errorf("parts = %v, want empty", parts)
	}
}

// TestSelectWinnerTiebreak covers the spec §4.5.4 examples explicitly.
func TestSelectWinnerTiebreak(t *testing.T) {
	cases := []struct {
		name   string
		counts []int
		want   int
	}{
		{"[3,4,3]", []int{3, 4, 3}, 0},
		{"[5,3,4]", []int{5, 3, 4}, 1},
		{"[5,3,5]", []int{5, 3, 5}, 1},
		{"[5,3,4,2]", []int{5, 3, 4, 2}, 3},
		{"[3,3,3]", []int{3, 3, 3}, 0},
		{"[5,4,3,2]", []int{5, 4, 3, 2}, 3},
		{"[3,2,4,2]", []int{3, 2, 4, 2}, 1},
		{"[4,2,9]", []int{4, 2, 9}, 1}, // Example 5
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Build fake components c0, c1, ... matching the counts array.
			components := make([]string, len(tc.counts))
			lookup := func(comp string) int {
				for i := range components {
					if components[i] == comp {
						return tc.counts[i]
					}
				}
				return 0
			}
			for i := range components {
				components[i] = string(rune('a' + i))
			}
			got, _ := selectWinner(components, lookup, 10)
			if got != tc.want {
				t.Errorf("winner = %d, want %d (counts=%v)", got, tc.want, tc.counts)
			}
		})
	}
}
