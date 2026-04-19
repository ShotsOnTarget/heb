package consolidate

import "testing"

func TestDeriveEventFromResult(t *testing.T) {
	cases := []struct {
		result string
		want   string
	}{
		{"matched", "prediction_confirmed"},
		{"wrong", "prediction_contradicted"},
		{"partial", ""},
		{"missed", ""},
		{"", ""},
		{"garbage", ""},
	}
	for _, c := range cases {
		got := DeriveEventFromResult(c.result)
		if got != c.want {
			t.Errorf("DeriveEventFromResult(%q) = %q, want %q", c.result, got, c.want)
		}
	}
}

// Normalisation must overwrite whatever Event the LLM supplied, including
// an explicit wrong value. The LLM is not the source of truth for Event.
func TestNormalisePredictionReconciliationOverwritesLLMEvent(t *testing.T) {
	pr := &PredictionReconciliation{
		Elements: []PredictionReconcileElement{
			{Element: "files", Result: "matched", Event: "prediction_contradicted"},
			{Element: "approach", Result: "wrong", Event: ""},
			{Element: "outcome", Result: "partial", Event: "prediction_confirmed"},
			{Element: "risks", Result: "missed", Event: "prediction_confirmed"},
		},
	}
	NormalisePredictionReconciliation(pr)

	want := []string{"prediction_confirmed", "prediction_contradicted", "", ""}
	for i, w := range want {
		if pr.Elements[i].Event != w {
			t.Errorf("Elements[%d].Event = %q, want %q", i, pr.Elements[i].Event, w)
		}
	}
}

func TestNormalisePredictionReconciliationNilSafe(t *testing.T) {
	NormalisePredictionReconciliation(nil) // must not panic
}
