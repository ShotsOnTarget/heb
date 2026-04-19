package main

import (
	"strings"
	"testing"
)

func TestRenderReconciliation_EmptyInputReturnsEmpty(t *testing.T) {
	if got := renderReconciliation(""); got != "" {
		t.Errorf("empty string: got %q, want empty", got)
	}
	if got := renderReconciliation("   "); got != "" {
		t.Errorf("whitespace: got %q, want empty", got)
	}
	if got := renderReconciliation("not json"); got != "" {
		t.Errorf("invalid json: got %q, want empty", got)
	}
}

func TestRenderReconciliation_ConfirmsWithNoSignalReturnsEmpty(t *testing.T) {
	in := `{"status":"confirms","conflicts":[],"extensions":[],"notes":"nothing to reconcile","proceed":true}`
	if got := renderReconciliation(in); got != "" {
		t.Errorf("confirms-no-signal should omit block entirely: got %q", got)
	}
}

func TestRenderReconciliation_OnlySupersededReturnsEmpty(t *testing.T) {
	// Superseded memories are already dropped from the memory list by
	// FilterSuperseded — re-listing them here would be noise.
	in := `{"conflicts":[
		{"existing_tuple":"old foo","conflict_type":"superseded","confidence":0.9}
	]}`
	if got := renderReconciliation(in); got != "" {
		t.Errorf("superseded-only should be absent: got %q", got)
	}
}

func TestRenderReconciliation_ExplicitConflictIncluded(t *testing.T) {
	in := `{"conflicts":[
		{"existing_tuple":"X uses pattern A","conflict_type":"explicit_update","new_value":"X now uses pattern B","confidence":0.85}
	]}`
	got := renderReconciliation(in)
	if !strings.Contains(got, "## Memory reconciliation") {
		t.Errorf("header missing: %q", got)
	}
	if !strings.Contains(got, "explicit_update") {
		t.Errorf("conflict type missing: %q", got)
	}
	if !strings.Contains(got, "X uses pattern A") || !strings.Contains(got, "X now uses pattern B") {
		t.Errorf("conflict tuple/new_value missing: %q", got)
	}
	if !strings.Contains(got, "0.85") {
		t.Errorf("confidence missing: %q", got)
	}
	if !strings.Contains(got, "Conflicts (prompt overrides these memories)") {
		t.Errorf("conflicts subheader missing: %q", got)
	}
}

func TestRenderReconciliation_ImplicitConflictIncluded(t *testing.T) {
	in := `{"conflicts":[
		{"existing_tuple":"A","conflict_type":"implicit_update","new_value":"B","confidence":0.75}
	]}`
	got := renderReconciliation(in)
	if !strings.Contains(got, "implicit_update") {
		t.Errorf("implicit_update must be surfaced: %q", got)
	}
}

func TestRenderReconciliation_ExtensionIncluded(t *testing.T) {
	in := `{"extensions":[
		{"existing_tuple":"Y handles red","extension":"prompt adds blue handling"}
	]}`
	got := renderReconciliation(in)
	if !strings.Contains(got, "Extensions (prompt adds beyond memory)") {
		t.Errorf("extensions subheader missing: %q", got)
	}
	if !strings.Contains(got, "Y handles red") || !strings.Contains(got, "prompt adds blue handling") {
		t.Errorf("extension content missing: %q", got)
	}
}

func TestRenderReconciliation_PredictionAndNotesAreExcluded(t *testing.T) {
	// Prediction fields are the falsifiable hypotheses /learn reconciles
	// against execute's actual behaviour. Showing them to execute
	// contaminates that measurement. Notes can leak prediction framing.
	in := `{
		"status":"conflicts",
		"conflicts":[{"existing_tuple":"A","conflict_type":"implicit_update","new_value":"B","confidence":0.75}],
		"extensions":[],
		"prediction":{
			"cold_start":false,
			"overall":0.8,
			"files":[{"path":"foo.go","confidence":"high","source_tuples":["t"]}],
			"approach":{"summary":"call doFoo() in main","confidence":"medium","source_tuples":["t"]},
			"outcome":{"summary":"go test ./... passes","confidence":"medium","source_tuples":["t"]},
			"risks":[{"risk":"NULL pointer in doFoo","confidence":"low","source_tuples":["t"]}]
		},
		"notes":"approach likely correct, high signal",
		"proceed":true
	}`
	got := renderReconciliation(in)

	if !strings.Contains(got, "implicit_update") {
		t.Fatalf("conflict signal must be preserved: %q", got)
	}
	forbidden := []string{
		"call doFoo",
		"foo.go",
		"cold_start",
		"overall",
		"NULL pointer",
		"go test",
		"high signal",
		"approach likely",
		"source_tuples",
		"prediction",
	}
	for _, bad := range forbidden {
		if strings.Contains(strings.ToLower(got), strings.ToLower(bad)) {
			t.Errorf("forbidden prediction/notes content leaked (%q): %q", bad, got)
		}
	}
}

func TestRenderReconciliation_MixedConflictsFilterSuperseded(t *testing.T) {
	in := `{"conflicts":[
		{"existing_tuple":"gone memory","conflict_type":"superseded","confidence":0.9},
		{"existing_tuple":"X=1","conflict_type":"explicit_update","new_value":"X=2","confidence":0.85}
	]}`
	got := renderReconciliation(in)
	if strings.Contains(got, "gone memory") {
		t.Errorf("superseded must be filtered (already dropped from memories): %q", got)
	}
	if !strings.Contains(got, "X=1") || !strings.Contains(got, "X=2") {
		t.Errorf("explicit conflict must pass through: %q", got)
	}
}

func TestRenderReconciliation_OutputTrailsWithNewline(t *testing.T) {
	// The combined prompt concatenates sections with no interstitial
	// spacing, so each rendered section must supply its own trailing
	// newline to produce a clean paragraph break.
	in := `{"conflicts":[{"existing_tuple":"A","conflict_type":"explicit_update","new_value":"B","confidence":0.8}]}`
	got := renderReconciliation(in)
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("output must end with newline for section spacing: %q", got)
	}
}

func TestRenderReconciliation_EmptyTupleSkipped(t *testing.T) {
	in := `{"conflicts":[
		{"existing_tuple":"","conflict_type":"explicit_update","new_value":"phantom","confidence":0.5},
		{"existing_tuple":"real","conflict_type":"explicit_update","new_value":"updated","confidence":0.8}
	]}`
	got := renderReconciliation(in)
	if strings.Contains(got, "phantom") {
		t.Errorf("malformed entry with empty existing_tuple should be skipped: %q", got)
	}
	if !strings.Contains(got, "real") {
		t.Errorf("valid entry must be rendered: %q", got)
	}
}
