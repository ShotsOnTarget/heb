package consolidate

import (
	"strings"

	"github.com/steelboltgames/heb/internal/memory"
)

// buildEntanglementDeltas emits negative memoryDelta entries for written
// tuples that overlap with surprise_touches.
//
// Gated by: len(surprise_touches) > 0 AND correction_count > 0.
//
// Signal is computed as -(peak_intensity * EntanglementScale), clamped
// into [EntanglementMin, EntanglementMax] (both negative; min is the
// most negative).
func buildEntanglementDeltas(written []MemoryDelta, c LearnResult, cfg Config) []MemoryDelta {
	if len(c.Implementation.SurpriseTouches) == 0 || c.CorrectionCount == 0 {
		return nil
	}
	if len(written) == 0 {
		return nil
	}

	signal := -(c.PeakIntensity * cfg.EntanglementScale)
	if signal < cfg.EntanglementMin {
		signal = cfg.EntanglementMin
	}
	if signal > cfg.EntanglementMax {
		signal = cfg.EntanglementMax
	}

	var out []MemoryDelta
	for _, m := range written {
		match, touch := matchesAnySurpriseTouch(m, c.Implementation.SurpriseTouches)
		if !match {
			continue
		}
		out = append(out, MemoryDelta{
			Body:           m.Body,
			Event:          "entanglement_signal",
			DeltaNew:       signal,
			DeltaReinforce: signal,
			Reason:         "surprise touch on " + touch,
		})
	}
	return out
}

// matchesAnySurpriseTouch reports whether any of the memory's body tokens
// appear as a case-insensitive substring of any surprise_touch path.
func matchesAnySurpriseTouch(m MemoryDelta, touches []string) (bool, string) {
	tokens := memory.Tokenize(m.Body)
	for _, touch := range touches {
		touchLower := strings.ToLower(touch)
		for _, tok := range tokens {
			if strings.Contains(touchLower, tok) {
				return true, touch
			}
		}
	}
	return false, ""
}
