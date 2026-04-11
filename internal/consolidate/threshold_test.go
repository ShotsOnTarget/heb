package consolidate

import "testing"

func TestThreshold(t *testing.T) {
	cases := []struct {
		name     string
		in       Contract4
		wantMet  bool
		wantSubs string // substring that must appear in reason
	}{
		{
			name:     "corrections trigger",
			in:       Contract4{CorrectionCount: 1, Completed: true},
			wantMet:  true,
			wantSubs: "correction_count",
		},
		{
			name:     "not completed triggers",
			in:       Contract4{Completed: false},
			wantMet:  true,
			wantSubs: "completed == false",
		},
		{
			name:     "peak intensity triggers",
			in:       Contract4{Completed: true, PeakIntensity: 0.31},
			wantMet:  true,
			wantSubs: "peak_intensity",
		},
		{
			name:     "peak intensity at 0.3 does NOT trigger",
			in:       Contract4{Completed: true, PeakIntensity: 0.30},
			wantMet:  false,
			wantSubs: "no significant signal",
		},
		{
			name: "files_touched triggers",
			in: Contract4{
				Completed:      true,
				Implementation: Implementation{FilesTouched: []string{"game/main.gd"}},
			},
			wantMet:  true,
			wantSubs: "files_touched",
		},
		{
			name: "lessons trigger",
			in: Contract4{
				Completed: true,
				Lessons:   []Lesson{{Observation: "a·b·c", Confidence: 0.8}},
			},
			wantMet:  true,
			wantSubs: "lessons",
		},
		{
			name:     "empty understand session — no trigger",
			in:       Contract4{Completed: true},
			wantMet:  false,
			wantSubs: "no significant signal",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			met, reason := checkThreshold(tc.in)
			if met != tc.wantMet {
				t.Errorf("met = %v, want %v", met, tc.wantMet)
			}
			if !contains(reason, tc.wantSubs) {
				t.Errorf("reason = %q, want substring %q", reason, tc.wantSubs)
			}
		})
	}
}

func contains(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
