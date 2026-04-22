package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/steelboltgames/heb/internal/retrieve"
	"github.com/steelboltgames/heb/internal/store"
)

// reflectSystemPrompt is the reflect contract, loaded from prompts/reflect.txt.
//
//go:embed prompts/reflect.txt
var reflectSystemPrompt string

// reflectResult is the parsed reflect contract output.
type reflectResult struct {
	SessionID  string          `json:"session_id"`
	Status     string          `json:"status"`
	Conflicts  json.RawMessage `json:"conflicts"`
	Extensions json.RawMessage `json:"extensions"`
	Prediction json.RawMessage `json:"prediction"`
	Notes      string          `json:"notes"`
	Proceed    bool            `json:"proceed"`
}

// doReflect runs the reflect step: reconciles retrieved memories against
// the prompt via LLM. Returns the result and its JSON.
func doReflect(sense *senseResult, ret *retrieve.Result) (*reflectResult, string, error) {
	// Build the user prompt with context
	var userPrompt strings.Builder
	fmt.Fprintf(&userPrompt, "## Original prompt\n\n%s\n\n", sense.Raw)

	now := time.Now().Unix()
	fmt.Fprintf(&userPrompt, "## Retrieved memories (%d)\n\n", len(ret.Memories))
	if len(ret.Memories) == 0 {
		userPrompt.WriteString("(none — cold start)\n\n")
	} else {
		for _, m := range ret.Memories {
			age := int((now - m.UpdatedAt) / 86400)
			if age < 0 {
				age = 0
			}
			fmt.Fprintf(&userPrompt, "- %s (weight: %.2f, source: %s, age: %dd)\n",
				m.TupleString(), m.Weight, m.Source, age)
		}
		userPrompt.WriteString("\n")
	}

	// Pre-compute pairwise similarity hints so the LLM confirms rather than discovers
	similarPairs := retrieve.FindSimilarPairs(ret.Memories, 0.70)
	if len(similarPairs) > 0 {
		fmt.Fprintf(&userPrompt, "## Supersession hints (high token overlap, different bodies)\n\n")
		for _, p := range similarPairs {
			olderAge := int((now - p.Older.UpdatedAt) / 86400)
			newerAge := int((now - p.Newer.UpdatedAt) / 86400)
			fmt.Fprintf(&userPrompt, "- CANDIDATE: %q (%dd) may be superseded by %q (%dd) — jaccard=%.2f\n",
				p.Older.Body, olderAge, p.Newer.Body, newerAge, p.Jaccard)
		}
		userPrompt.WriteString("\n")
	}

	fmt.Fprintf(&userPrompt, "## Git refs (%d)\n\n", len(ret.GitRefs))
	for _, g := range ret.GitRefs {
		fmt.Fprintf(&userPrompt, "- %s %s (%s)\n", g.Hash, g.Message, g.Age)
	}
	if len(ret.GitRefs) == 0 {
		userPrompt.WriteString("(none)\n")
	}
	userPrompt.WriteString("\n")

	fmt.Fprintf(&userPrompt, "## Beads refs (%d)\n\n", len(ret.Beads))
	for _, b := range ret.Beads {
		fmt.Fprintf(&userPrompt, "- %s %s [%s]\n", b.ID, b.Title, b.Status)
	}
	if len(ret.Beads) == 0 {
		userPrompt.WriteString("(none)\n")
	}

	systemPrompt := strings.ReplaceAll(reflectSystemPrompt, "<SESSION_ID>", sense.SessionID)

	// Call LLM: honor reflect.model config, then fall back to auto-detected provider.
	var raw string
	var usage apiUsage
	var err error
	provider, apiKey, model := resolveModel("reflect")
	start := time.Now()
	switch provider {
	case "anthropic":
		fmt.Fprintf(os.Stderr, "reflecting [%s]...\n", modelLabel(model, defaultAnthropicModel))
		raw, usage, err = senseViaAnthropic(apiKey, model, systemPrompt, userPrompt.String(), 2048)
	case "openai":
		fmt.Fprintf(os.Stderr, "reflecting [%s]...\n", modelLabel(model, defaultOpenAIModel))
		raw, usage, err = senseViaOpenAI(apiKey, model, systemPrompt, userPrompt.String(), 2048)
	default:
		fmt.Fprintf(os.Stderr, "reflecting [%s]...\n", modelLabel(model, "claude"))
		cwd, _ := os.Getwd()
		raw, usage, err = senseViaClaude(cwd, model, systemPrompt, userPrompt.String())
	}
	elapsed := time.Since(start)
	if err != nil {
		return nil, "", fmt.Errorf("reflect: %w", err)
	}
	emitStats("reflect", usage, elapsed)

	raw = stripJSONFences(strings.TrimSpace(raw))

	var result reflectResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, "", fmt.Errorf("invalid JSON from reflect: %v\nraw: %s", err, raw)
	}

	// Enforce session_id
	result.SessionID = sense.SessionID
	corrected, _ := json.Marshal(result)
	raw = string(corrected)

	// Emit reconcile detail lines for the GUI
	var nConflicts, nExtensions int
	if result.Conflicts != nil {
		var conflicts []json.RawMessage
		json.Unmarshal(result.Conflicts, &conflicts)
		nConflicts = len(conflicts)
	}
	if result.Extensions != nil {
		var extensions []json.RawMessage
		json.Unmarshal(result.Extensions, &extensions)
		nExtensions = len(extensions)
	}
	fmt.Fprintf(os.Stderr, "reconcile: %s — %d conflicts, %d extensions\n", result.Status, nConflicts, nExtensions)
	if result.Notes != "" {
		fmt.Fprintf(os.Stderr, "reconcile-notes: %s\n", result.Notes)
	}
	// Emit individual conflicts
	if nConflicts > 0 {
		var conflicts []struct {
			ExistingTuple string  `json:"existing_tuple"`
			ConflictType  string  `json:"conflict_type"`
			NewValue      string  `json:"new_value"`
			Confidence    float64 `json:"confidence"`
		}
		json.Unmarshal(result.Conflicts, &conflicts)
		for _, c := range conflicts {
			fmt.Fprintf(os.Stderr, "reconcile-conflict: [%s %.2f] %s → %s\n", c.ConflictType, c.Confidence, c.ExistingTuple, c.NewValue)
		}
	}

	// Emit predict detail lines for the GUI
	var pred struct {
		ColdStart bool    `json:"cold_start"`
		Overall   float64 `json:"overall"`
		Files     []struct {
			Path       string `json:"path"`
			Confidence string `json:"confidence"`
		} `json:"files"`
		Approach struct {
			Summary    string `json:"summary"`
			Confidence string `json:"confidence"`
		} `json:"approach"`
		Outcome struct {
			Summary    string `json:"summary"`
			Confidence string `json:"confidence"`
		} `json:"outcome"`
		Risks []struct {
			Risk       string `json:"risk"`
			Confidence string `json:"confidence"`
		} `json:"risks"`
	}
	if result.Prediction != nil {
		json.Unmarshal(result.Prediction, &pred)
	}
	if pred.ColdStart {
		fmt.Fprintf(os.Stderr, "predict: cold start (overall: %.2f)\n", pred.Overall)
	} else {
		fmt.Fprintf(os.Stderr, "predict: overall confidence %.2f\n", pred.Overall)
		for _, f := range pred.Files {
			fmt.Fprintf(os.Stderr, "predict-file: %s (%s)\n", f.Path, f.Confidence)
		}
		if pred.Approach.Summary != "" {
			fmt.Fprintf(os.Stderr, "predict-approach: %s (%s)\n", pred.Approach.Summary, pred.Approach.Confidence)
		}
		if pred.Outcome.Summary != "" {
			fmt.Fprintf(os.Stderr, "predict-outcome: %s (%s)\n", pred.Outcome.Summary, pred.Outcome.Confidence)
		}
		for _, r := range pred.Risks {
			fmt.Fprintf(os.Stderr, "predict-risk: %s (%s)\n", r.Risk, r.Confidence)
		}
	}

	// Persist to session (best-effort)
	{
		s, err := store.Open()
		if err == nil {
			defer s.Close()
			if err := store.WriteContract(s.DB(), sense.SessionID, "reflect", raw); err != nil {
				fmt.Fprintf(os.Stderr, "heb: session write reflect: %v\n", err)
			}
		}
	}

	return &result, raw, nil
}

// runReflect is the `heb reflect` entry point.
func runReflect(args []string) int {
	prompt := strings.Join(args, " ")
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "usage: heb reflect <prompt>")
		return 2
	}

	sense, _, err := doSense(prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: %v\n", err)
		return 1
	}

	ret, _, err := doRetrieve(sense)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: %v\n", err)
		return 1
	}

	_, jsonOut, err := doReflect(sense, ret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: %v\n", err)
		return 1
	}

	fmt.Fprintln(os.Stdout, jsonOut)
	return 0
}
