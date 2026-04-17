package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

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
2. Retrieved memory tuples with weights and age (days since last reinforced)
3. Retrieved git refs and beads refs

## Reconciliation

### Step 1: Memory-vs-memory conflicts

Before comparing memories to the prompt, check whether any retrieved memories contradict **each other**. When two memories cover the same topic with different values, the more recent one (lower age) supersedes the older. Flag the older memory as conflict_type "superseded" — Execute should ignore it and trust the newer one.

### Step 2: Memory-vs-prompt conflicts

For each non-superseded memory, classify as:

**CONFIRMS** (default) — prompt is consistent with memory, or memory is irrelevant to the prompt.

**EXTENDS** — prompt adds something new to the subject area without contradicting it.

**CONFLICTS** — prompt contradicts the memory:
- explicit_update: prompt directly states a different value (confidence 0.85)
- implicit_update: applying the memory would make the task wrong (confidence 0.75)
- superseded: an older memory was replaced by a newer memory on the same topic (confidence 0.90)
Drop conflicts below confidence 0.50.

The prompt always wins over memory. Between memories, newer wins over older.

If no memories were retrieved, status is "confirms" with notes "cold start — nothing to reconcile against".

## Prediction

After conflict detection, predict what will happen during execution. The prediction is the memory graph's model of reality expressed as falsifiable statements. After execution, /learn reconciles each element against what actually happened. Matched predictions strengthen source memories. Wrong predictions weaken them. Vague predictions that can't be cleanly classified as matched or wrong pollute the weight signal — they are worse than no prediction at all.

### Hard rule: specificity or silence

Every prediction element must be specific enough that /learn can mark it "matched" or "wrong" without ambiguity. If you don't have enough signal to be specific, set cold_start: true and leave element arrays/summaries empty. A cold-start prediction that says "I don't know" is more valuable than a hedged prediction that says "probably something in the combat system".

cold_start is true when EITHER: (1) no memories were retrieved, OR (2) memories were retrieved but none of them inform any prediction element — i.e., you cannot populate source_tuples for any element. Retrieved memories that are irrelevant to the prompt's task do not count as signal. One condition, one rule: if you can't trace prediction elements to specific memories, it's a cold start.

### Hard rule: source_tuples required

Every non-cold-start prediction element MUST have at least one entry in source_tuples. source_tuples are the verbatim body strings of the retrieved memories that informed the prediction — copy them exactly as they appear in the input. A prediction not traceable to a specific memory cannot produce a weight delta when contradicted. If no memory informs an element, omit that element entirely rather than filling it with unsourced guesses.

### Element specificity requirements

**files** — actual file paths or glob patterns. Not descriptions of areas.
- BAD: "probably something in the combat system" (not a path, not falsifiable)
- BAD: "files related to drone cost calculation" (description, not a path)
- GOOD: "cmd/heb/reflect.go" (exact path, trivially verifiable)
- GOOD: "internal/consolidate/*.go" (glob pattern, verifiable)
- GOOD: "commands/remember.md, commands/heb.md" (specific set)

**approach** — a concrete technical statement about HOW the task will be accomplished. Must name specific functions, patterns, or operations.
- BAD: "implement the feature" (restates the prompt)
- BAD: "modify the relevant files" (says nothing)
- GOOD: "add a new case to the switch in runCommand() in main.go, wire it to a new doFoo() function following the doReflect pattern"
- GOOD: "replace the hardcoded threshold with a config value read from heb.toml via loadConfig()"

**outcome** — an observable end state that can be verified by inspecting code, running tests, or checking behavior. Must not simply restate the prompt.
- BAD: "the feature will work as expected" (tautology)
- BAD: "a list of files with summaries" (restates the prompt)
- GOOD: "go test ./internal/consolidate/... passes with the new threshold case covered"
- GOOD: "heb status output includes a new 'dreams' line showing count and last-dream timestamp"

**risks** — specific failure modes with triggering conditions. Each risk must name WHAT could fail and UNDER WHAT CONDITION. Empty risks is only correct when no retrieved memory suggests a failure mode. If a memory describes a pattern, convention, or constraint that the prompt's task could violate or interact with badly, that IS a risk — name it.
- BAD: "some files may be complex" (not a failure mode)
- BAD: "there might be edge cases" (always true, not useful)
- GOOD: "if memories table has NULL updated_at rows, the age calculation in doReflect will panic on integer division"
- GOOD: "the BM25 scorer in retrieve.go uses TF-IDF weights that may not rank the new token type correctly — existing tests don't cover multi-word tokens"

### Confidence

Per element: high (>0.80), medium (0.50-0.80), low (<0.50). Overall is the MINIMUM of element confidences, not the average.

### Cold start output

When cold_start is true: files is an empty array, approach and outcome have empty summaries, risks is an empty array, overall is below 0.30.

## Output shape

{
  "session_id": "<SESSION_ID>",
  "status": "confirms | extends | conflicts",
  "conflicts": [
    {
      "existing_tuple":  "...",
      "existing_weight": 0.0,
      "conflict_type":   "explicit_update | implicit_update | superseded",
      "new_value":       "...",
      "superseded_by":   "...(tuple of the newer memory, if superseded)...",
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
    "files": [{"path": "cmd/heb/reflect.go", "confidence": "high", "source_tuples": ["internal/retrieve/ implements BM25_IDF_memory_retrieval"]}],
    "approach": {"summary": "concrete technical statement naming functions and patterns", "confidence": "medium", "source_tuples": ["consolidate.Run implements pure transformation of weight deltas"]},
    "outcome": {"summary": "observable verifiable end state", "confidence": "medium", "source_tuples": ["session energy budget 120 tokens caps write volume per learn session"]},
    "risks": [{"risk": "specific failure mode with triggering condition", "confidence": "low", "source_tuples": ["memory.Tokenize splits on space underscore hyphen dot slash middledot"]}],
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

	// Call LLM
	var raw string
	var err error
	provider, apiKey := resolveProvider()
	switch provider {
	case "anthropic":
		fmt.Fprintf(os.Stderr, "reflecting [haiku]...\n")
		raw, err = senseViaAnthropic(apiKey, systemPrompt, userPrompt.String(), 2048)
	case "openai":
		fmt.Fprintf(os.Stderr, "reflecting [gpt-4.1-mini]...\n")
		raw, err = senseViaOpenAI(apiKey, systemPrompt, userPrompt.String(), 2048)
	default:
		fmt.Fprintf(os.Stderr, "reflecting [claude]...\n")
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
