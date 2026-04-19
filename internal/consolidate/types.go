// Package consolidate translates contract:learn>consolidate (the output of /learn) into
// the explicit delta payload that the heb store can apply. It is pure —
// no filesystem, no sqlite — so it can be unit-tested in isolation.
//
// The translation happens in four passes:
//
//	1. threshold   — does the session meet significance criteria?
//	2. memories    — lessons → memoryDelta entries
//	3. edges       — pair enumeration over the written set
//	4. entanglement — surprise_touches → negative memoryDelta entries
//
// Run() wires them together and returns a Result. The caller (cmd/heb)
// applies the Result to the store inside its own transaction.
package consolidate

import (
	"encoding/json"
	"strings"
)

// Config holds all tunable constants. Defaults match the existing
// consolidate.md math exactly.
type Config struct {
	NewGain           float64 // delta for created memories (confidence × this)
	ReinforceGain     float64 // delta for existing memories (confidence × this)
	CoActivationBoost float64 // edge delta when both tuples written together
	EntanglementScale float64 // signal = -(peak_intensity × this)
	EntanglementMin   float64 // clamp lower bound (most negative)
	EntanglementMax        float64 // clamp upper bound (closest to zero)
	EdgeDecayRate          float64 // negative delta per session for unconfirmed edge recalls
	EdgeEstablishThreshold int     // co_activation_count >= this means edge is established
	MinConfidence          float64 // drop lessons below this confidence
	Format                 string  // "both" | "human" | "json"
}

// DefaultConfig returns the Hebbian constants baked into the original
// consolidate.md markdown spec.
func DefaultConfig() Config {
	return Config{
		NewGain:           0.72,
		ReinforceGain:     0.08,
		CoActivationBoost: 0.06,
		EntanglementScale: 0.05,
		EntanglementMin:   -0.08,
		EntanglementMax:        -0.02,
		EdgeDecayRate:          0.005,
		EdgeEstablishThreshold: 3,
		MinConfidence:          0.50,
		Format:            "both",
	}
}

// LearnResult is the parsed /learn output.
type LearnResult struct {
	SessionID        string          `json:"session_id"`
	BeadID           string          `json:"bead_id,omitempty"`
	Project          string          `json:"project"`
	Intent           string          `json:"intent,omitempty"` // deprecated: unused, kept for backwards compat with existing episodes
	Tokens           []string        `json:"tokens,omitempty"`
	MemoryLoaded     json.RawMessage `json:"memory_loaded,omitempty"`
	Implementation   Implementation  `json:"implementation"`
	CorrectionCount  int             `json:"correction_count"`
	PeakIntensity    float64         `json:"peak_intensity"`
	Completed        bool            `json:"completed"`
	Decisions        []Decision        `json:"decisions,omitempty"`
	Lessons          []Lesson        `json:"lessons"`
	RecalledViaEdges          FlexStringSlice          `json:"recalled_via_edges,omitempty"`
	ConfirmedRecalls         FlexStringSlice          `json:"confirmed_recalls,omitempty"`
	PredictionReconciliation *PredictionReconciliation `json:"prediction_reconciliation,omitempty"`

	// Raw holds the full original payload so the episode row can
	// preserve everything, including fields the CLI does not interpret.
	Raw json.RawMessage `json:"-"`
}

// PredictionReconciliation is the learn contract's reconciliation of
// reflect predictions against what actually happened.
type PredictionReconciliation struct {
	ColdStart    bool                        `json:"cold_start"`
	Elements     []PredictionReconcileElement `json:"elements"`
	MatchedCount int                          `json:"matched_count"`
	TotalCount   int                          `json:"total_count"`
	Overall      string                       `json:"overall"`
	Summary      string                       `json:"summary"`
}

// PredictionReconcileElement is one predicted-vs-actual comparison.
type PredictionReconcileElement struct {
	Element      string          `json:"element"`
	Predicted    FlexString      `json:"predicted"`
	Actual       FlexString      `json:"actual"`
	Result       string          `json:"result"` // "matched" | "partial" | "missed" | "wrong"
	SourceTuples FlexStringSlice `json:"source_tuples"`
	Event        string          `json:"event"` // "prediction_confirmed" | "prediction_contradicted" | ""
	Lesson       string          `json:"lesson,omitempty"`
}

// DeriveEventFromResult maps a reconciliation verdict to the event kind
// used downstream for weight deltas and event logging. The mapping is
// deterministic — the LLM's own "event" field is ignored so an omission
// or mislabelling can't silently suppress the Hebbian loop.
//
//   matched → prediction_confirmed
//   wrong   → prediction_contradicted
//   partial → ""   (hedged verdict: no weight signal)
//   missed  → ""   (handled via lesson extraction, not source-tuple delta)
func DeriveEventFromResult(result string) string {
	switch result {
	case "matched":
		return "prediction_confirmed"
	case "wrong":
		return "prediction_contradicted"
	default:
		return ""
	}
}

// NormalisePredictionReconciliation overwrites each element's Event from
// its Result via the deterministic mapping in DeriveEventFromResult.
// Safe to call with nil.
func NormalisePredictionReconciliation(pr *PredictionReconciliation) {
	if pr == nil {
		return
	}
	for i := range pr.Elements {
		pr.Elements[i].Event = DeriveEventFromResult(pr.Elements[i].Result)
	}
}

// FlexString unmarshals both "string" and ["array","of","strings"] into a single string.
// Arrays are joined with ", ".
type FlexString string

func (f *FlexString) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = FlexString(s)
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*f = FlexString(strings.Join(arr, ", "))
		return nil
	}
	// Fallback: use raw string
	*f = FlexString(string(data))
	return nil
}

// FlexStringSlice unmarshals both ["s·p·o"] and [["s","p","o"]] into []string.
// When an element is an array of strings, the parts are joined with "·".
type FlexStringSlice []string

func (f *FlexStringSlice) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	result := make([]string, 0, len(raw))
	for _, elem := range raw {
		var s string
		if err := json.Unmarshal(elem, &s); err == nil {
			result = append(result, s)
			continue
		}
		var arr []string
		if err := json.Unmarshal(elem, &arr); err == nil {
			result = append(result, strings.Join(arr, "\u00b7"))
			continue
		}
	}
	*f = result
	return nil
}

// Implementation is the nested block tracking file operations and
// approach notes.
type Implementation struct {
	FilesTouched    []string `json:"files_touched"`
	FilesRead       []string `json:"files_read,omitempty"`
	SurpriseTouches []string `json:"surprise_touches"`
	Approach        string   `json:"approach,omitempty"`
}

// Lesson is one observation the agent learned during the session.
type Lesson struct {
	Body       string  `json:"body"`              // free-form atom text
	Scope      string  `json:"scope"`             // "project" | "universal_candidate"
	Confidence float64 `json:"confidence"`
	Evidence   string  `json:"evidence,omitempty"`
}

// UnmarshalJSON handles backward compatibility with the old "observation" field.
func (l *Lesson) UnmarshalJSON(data []byte) error {
	type Alias Lesson
	aux := &struct {
		Observation string `json:"observation"`
		*Alias
	}{Alias: (*Alias)(l)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if l.Body == "" && aux.Observation != "" {
		l.Body = aux.Observation
	}
	return nil
}

// Decision is a design question the agent asked and the developer answered.
type Decision struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Weight   string `json:"weight"` // "high" | "medium" | "low"
}

// MemoryDelta is a single memory event to apply to the store. The store
// picks deltaNew or deltaReinforce based on whether the memory already
// exists. For entanglement events both deltas are the same negative
// number.
type MemoryDelta struct {
	Body           string  `json:"body"`
	Event          string  `json:"event"` // "session_reinforced" | "entanglement_signal"
	DeltaNew       float64 `json:"delta_new"`
	DeltaReinforce float64 `json:"delta_reinforce"`
	Reason         string  `json:"reason,omitempty"`
}

// EdgeDelta is a strengthening between two memories written in the same
// session. Canonicalisation (smaller ID first) is handled by the store.
type EdgeDelta struct {
	ABody        string  `json:"a_body"`
	BBody        string  `json:"b_body"`
	Delta        float64 `json:"delta"`
	CoActivation bool    `json:"co_activation,omitempty"`
}

// SkippedTuple captures a lesson that was dropped by the memories pass.
type SkippedTuple struct {
	Tuple  string `json:"tuple"`
	Reason string `json:"reason"`
}

// Payload is the explicit delta payload (memories + edges + episode +
// skipped) produced by the translator. This is the shape cmd/heb
// applies to the store. It is also the shape accepted by --raw mode.
type Payload struct {
	SessionID   string          `json:"session_id"`
	Project     string          `json:"project"`
	BeadID      string          `json:"bead_id,omitempty"`
	TopicTokens string          `json:"topic_tokens,omitempty"` // comma-separated sense tokens for memory tagging
	CommitHash  string          `json:"commit_hash,omitempty"`  // git HEAD at consolidate time — traceability
	Memories    []MemoryDelta   `json:"memories"`
	Edges       []EdgeDelta     `json:"edges"`
	Episode     *EpisodePayload `json:"episode,omitempty"`
	Skipped     []SkippedTuple  `json:"skipped,omitempty"`
}

// EpisodePayload mirrors the shape the store writes verbatim into the
// episodes table. Payload is the full contract:learn>consolidate JSON nested as-is.
type EpisodePayload struct {
	SessionID string          `json:"session_id"`
	Payload   json.RawMessage `json:"payload"`
}

// Result is the full consolidate output: the explicit Payload, the
// threshold verdict, and whatever errors the translator surfaced.
type Result struct {
	SessionID           string         `json:"session_id"`
	Project             string         `json:"project"`
	ThresholdMet        bool           `json:"threshold_met"`
	ThresholdReason     string         `json:"threshold_reason,omitempty"`
	Payload             Payload        `json:"-"`
	Applied                  []MemoryApply  `json:"applied"`
	EdgesUpdated             int            `json:"edges_updated"`
	EdgesDecayed             int            `json:"edges_decayed"`
	EntanglementSignals      int            `json:"entanglement_signals"`
	PredictionsConfirmed     int            `json:"predictions_confirmed"`
	PredictionsContradicted  int            `json:"predictions_contradicted"`
	PredictionsOrphaned      int            `json:"predictions_orphaned"` // source_tuples that did not resolve to an existing memory
	EpisodeWritten           bool           `json:"episode_written"`
	EpisodePath              string         `json:"episode_path,omitempty"`
	Skipped                  []SkippedTuple `json:"skipped"`
	Errors                   []string       `json:"errors"`
}

// MemoryApply is one row in Result.Applied — the post-store outcome of
// applying a MemoryDelta. Filled in by cmd/heb after the transaction.
type MemoryApply struct {
	ID        string  `json:"id"`
	Body      string  `json:"body"`
	Event     string  `json:"event"`
	NewWeight float64 `json:"new_weight"`
	WasNew    bool    `json:"was_new"`
}
