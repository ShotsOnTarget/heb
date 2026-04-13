package consolidate

// checkThreshold applies the §3.1 significance rule. Returns (met, reason).
// Reason is the first condition found true when met == true, or a
// human-readable "no signal" description when met == false.
//
// Spec: skip memory deltas if none of these are true:
//
//	correction_count > 0
//	completed == false
//	peak_intensity > 0.3
//	len(decisions) > 0
//	len(files_touched) > 0
//	len(lessons) > 0
func checkThreshold(c LearnResult) (bool, string) {
	switch {
	case c.CorrectionCount > 0:
		return true, "correction_count > 0"
	case !c.Completed:
		return true, "completed == false"
	case c.PeakIntensity > 0.3:
		return true, "peak_intensity > 0.3"
	case len(c.Decisions) > 0:
		return true, "decisions recorded"
	case len(c.Implementation.FilesTouched) > 0:
		return true, "files_touched non-empty"
	case len(c.Lessons) > 0:
		return true, "lessons recorded"
	default:
		return false, "no significant signal (no corrections, no files touched, no lessons, no decisions, low intensity)"
	}
}
