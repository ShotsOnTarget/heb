package consolidate

import "strings"

// buildEntanglementDeltas emits negative memoryDelta entries for written
// tuples that overlap with surprise_touches, per §3.4 of the yx0
// proposal.
//
// Gated by: len(surprise_touches) > 0 AND correction_count > 0.
//
// Signal is computed as -(peak_intensity * EntanglementScale), clamped
// into [EntanglementMin, EntanglementMax] (both negative; min is the
// most negative). The signal is applied to the written set only — the
// same tuples §3.3 emitted edges for.
//
// For each written tuple, its three parts are compared case-insensitively
// against every surprise_touch full path as a substring check. On match,
// an additional memoryDelta is emitted with event "entanglement_signal"
// and delta_new = delta_reinforce = signal. This is appended to, not
// merged with, any reinforcement delta already emitted in §3.2.
func buildEntanglementDeltas(written []MemoryDelta, c Contract4, cfg Config) []MemoryDelta {
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
			Subject:        m.Subject,
			Predicate:      m.Predicate,
			Object:         m.Object,
			Event:          "entanglement_signal",
			DeltaNew:       signal,
			DeltaReinforce: signal,
			Reason:         "surprise touch on " + touch,
		})
	}
	return out
}

// matchesAnySurpriseTouch reports whether any of the tuple's three parts
// appears as a case-insensitive substring of any surprise_touch path.
// Returns the first matching path for use in the reason string.
func matchesAnySurpriseTouch(m MemoryDelta, touches []string) (bool, string) {
	parts := [3]string{
		strings.ToLower(m.Subject),
		strings.ToLower(m.Predicate),
		strings.ToLower(m.Object),
	}
	for _, touch := range touches {
		touchLower := strings.ToLower(touch)
		for _, part := range parts {
			if part == "" {
				continue
			}
			if strings.Contains(touchLower, part) {
				return true, touch
			}
		}
	}
	return false, ""
}
