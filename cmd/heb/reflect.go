package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/steelboltgames/heb/internal/retrieve"
	"github.com/steelboltgames/heb/internal/store"
)

const reflectSystemPrompt = `You are a memory reconciliation engine. Given a developer's prompt and retrieved memories, produce a structured JSON reconciliation.

## Hard rules

- DO NOT read files, run commands, or call any tools
- DO NOT try to solve the task or comment on it
- Output ONLY the JSON object — no markdown fences, no preamble, no commentary

## Inputs

You will receive:
1. The original prompt
2. Retrieved memory tuples with weights
3. Retrieved git refs and beads refs

## Reconciliation

For each memory, classify as:

**CONFIRMS** (default) — prompt is consistent with memory, or memory is irrelevant to the prompt.

**EXTENDS** — prompt adds something new to the subject area without contradicting it.

**CONFLICTS** — prompt contradicts the memory:
- explicit_update: prompt directly states a different value (confidence 0.85)
- implicit_update: applying the memory would make the task wrong (confidence 0.75)
Drop conflicts below confidence 0.50.

The prompt always wins over memory.

If no memories were retrieved, status is "confirms" with notes "cold start — nothing to reconcile against".

## Prediction

Predict what will happen during execution based on the prompt and memories:
- files: what files will be touched
- approach: what approach will work
- outcome: expected result
- risks: what could go wrong

Each element needs a confidence (high/medium/low) and source_tuples listing which memories informed it.

Cold start: low confidence, cold_start: true, overall below 0.30. Do not fabricate detail — say what you don't know.

## Output shape

{
  "session_id": "<SESSION_ID>",
  "status": "confirms | extends | conflicts",
  "conflicts": [
    {
      "existing_tuple":  "...",
      "existing_weight": 0.0,
      "conflict_type":   "explicit_update | implicit_update",
      "new_value":       "...",
      "confidence":      0.0,
      "action":          "create_successor"
    }
  ],
  "extensions": [
    {
      "existing_tuple": "...",
      "extension":      "..."
    }
  ],
  "prediction": {
    "cold_start": false,
    "files": [{"path": "...", "confidence": "high|medium|low", "source_tuples": []}],
    "approach": {"summary": "...", "confidence": "high|medium|low", "source_tuples": []},
    "outcome": {"summary": "...", "confidence": "high|medium|low", "source_tuples": []},
    "risks": [{"risk": "...", "confidence": "high|medium|low", "source_tuples": []}],
    "overall": 0.0
  },
  "notes": "...",
  "proceed": true
}`

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

	fmt.Fprintf(&userPrompt, "## Retrieved memories (%d)\n\n", len(ret.Memories))
	if len(ret.Memories) == 0 {
		userPrompt.WriteString("(none — cold start)\n\n")
	} else {
		for _, m := range ret.Memories {
			fmt.Fprintf(&userPrompt, "- %s (weight: %.2f, source: %s)\n",
				m.TupleString(), m.Weight, m.Source)
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

	fmt.Fprintf(os.Stderr, "reflecting...\n")

	// Call LLM
	var raw string
	var err error
	provider, apiKey := resolveProvider()
	switch provider {
	case "anthropic":
		raw, err = senseViaAnthropic(apiKey, systemPrompt, userPrompt.String())
	case "openai":
		raw, err = senseViaOpenAI(apiKey, systemPrompt, userPrompt.String())
	default:
		cwd, _ := os.Getwd()
		raw, err = senseViaClaude(cwd, systemPrompt, userPrompt.String())
	}
	if err != nil {
		return nil, "", fmt.Errorf("reflect: %w", err)
	}

	raw = stripJSONFences(strings.TrimSpace(raw))

	var result reflectResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, "", fmt.Errorf("invalid JSON from reflect: %v\nraw: %s", err, raw)
	}

	// Enforce session_id
	result.SessionID = sense.SessionID
	corrected, _ := json.Marshal(result)
	raw = string(corrected)

	// Display
	fmt.Fprintln(os.Stderr, "REFLECT")
	fmt.Fprintln(os.Stderr, "────────────────────────────────────────")
	fmt.Fprintf(os.Stderr, "status:      %s\n", result.Status)
	fmt.Fprintf(os.Stderr, "notes:       %s\n", result.Notes)
	fmt.Fprintf(os.Stderr, "proceed:     %v\n", result.Proceed)
	switch provider {
	case "anthropic":
		fmt.Fprintf(os.Stderr, "via:         anthropic api (haiku)\n")
	case "openai":
		fmt.Fprintf(os.Stderr, "via:         openai api (gpt-4.1-mini)\n")
	default:
		fmt.Fprintf(os.Stderr, "via:         claude --print\n")
	}
	fmt.Fprintln(os.Stderr, "────────────────────────────────────────")

	// Persist to session (best-effort)
	root, err := store.RepoRoot()
	if err == nil {
		s, err := store.Open(root)
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
