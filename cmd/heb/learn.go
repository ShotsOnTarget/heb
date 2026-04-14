package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/steelboltgames/heb/internal/consolidate"
	"github.com/steelboltgames/heb/internal/store"
)

const learnSystemPrompt = `You are a pure session learner. Your only job is to read a transcript of a developer's session with an AI agent and emit a single contract:learn>consolidate JSON object.

## Hard rules

- DO NOT propose new work, fixes, or follow-ups
- DO NOT touch the memory graph yourself
- DO NOT add fields, omit fields, or reshape the contract
- DO NOT re-derive tokens or intent — copy from the sense contract verbatim
- DO NOT invent decisions or corrections that did not happen
- DO NOT write lessons below 0.50 confidence
- DO NOT write lessons the session did not earn
- Output ONLY the JSON object — no markdown fences, no preamble, no commentary
- Complete in a single response

## Inputs

You will receive:
1. The sense contract (session metadata, tokens, intent)
2. The recall contract (retrieved memories, git refs)
3. The reflect contract (reconciliation, predictions)
4. The full transcript (user prompts and assistant responses)

## Output shape

{
  "session_id":     "from sense contract",
  "bead_id":        null,
  "project":        "from sense contract",
  "timestamp_end":  "<TIMESTAMP_END>",
  "raw_prompt":     "original prompt from sense.raw",
  "intent":         "from sense contract",
  "tokens":         ["from sense contract"],

  "memory_loaded": {
    "memories_loaded": 0,
    "git_refs":        0,
    "was_cold_start":  true
  },

  "recalled_via_edges": [],

  "implementation": {
    "files_touched":    [],
    "files_read":       [],
    "surprise_touches": [],
    "approach":         "one sentence past tense",
    "patterns_used":    []
  },

  "decisions": [],
  "corrections": [
    {
      "what": "what the agent did wrong",
      "correction": "what the developer said instead",
      "intensity": 0.5
    }
  ],
  "correction_count": 0,
  "peak_intensity":   0.0,
  "completed":        true,

  "lessons": [
    {
      "body":        "free-form text atom — terse, useful, greppable",
      "scope":       "project | universal_candidate",
      "confidence":  0.0,
      "evidence":    "what in the session supports this",
      "source":      "session | prediction"
    }
  ],

  "prediction_reconciliation": null
}

## Field rules

session_id — copy from sense contract verbatim.
bead_id — scan transcript for bd update, bd close, bd show commands. First bead id found. null if none.
project — from sense contract.
timestamp_end — use: <TIMESTAMP_END>
raw_prompt — original developer prompt from sense contract raw field.
intent, tokens — copy from sense contract. Do not re-derive.

memory_loaded — copy VERBATIM from the "Pre-computed fields" section. Do NOT re-derive.
recalled_via_edges — copy VERBATIM from the "Pre-computed fields" section. Do NOT re-derive.
prediction_reconciliation.elements[].source_tuples — copy from the "Pre-computed fields" section's prediction_source_tuples, matching by element type (files, approach, outcome, risks). Empty arrays are acceptable when no memories informed the prediction (e.g. cold start).

implementation.files_touched — copy VERBATIM from "Pre-computed fields" if present. Otherwise extract from transcript.
implementation.files_read — copy VERBATIM from "Pre-computed fields" if present. Otherwise extract from transcript. Superset of files_touched.
implementation.surprise_touches — files in files_read with no obvious connection to the tokens. Empty if intent was understand.
implementation.approach — one sentence, past tense, concrete.
implementation.patterns_used — architectural patterns actually applied. Empty if none.

decisions — every question the agent asked AND the developer answered. Each object has exactly three fields:
  - "question": the question the agent asked
  - "answer": what the developer answered
  - "weight": "high" (design decision), "medium" (clarification), "low" (confirmation)
Empty array if the agent asked nothing.

corrections — every developer correction. Each object has exactly three fields:
  - "what": what the agent did wrong or what needed correcting (past tense)
  - "correction": what the developer said or instructed instead
  - "intensity": 0.1-0.3 (minor), 0.4-0.6 (clear), 0.7-0.8 (emphatic), 0.9-1.0 (hard/caps/repetition)
Do NOT use "developer_input" or any other field name. Empty array if none.

correction_count — length of corrections array.
peak_intensity — max intensity, or 0.0.
completed — true if the task was finished.

lessons — what should be remembered. Each lesson body is free-form text — terse, useful, greppable. No forced structure. Max 8. Min 0. Confidence >= 0.50.

HARD SIZE RULE: Each atom MUST be ≤ 12 words. No exceptions. If an insight needs more words, split it into 2-3 separate atoms. Verbose atoms are mechanically penalised — the system multiplies confidence by (12 / word_count) for atoms exceeding 12 words, so a 24-word atom competes at half confidence. Write terse or get filtered.

Format: noun-verb-noun preferred. No filler words (implements, with, functions for, as a, the, etc.). Good: "CombatScreen syncs combat state". Bad: "The CombatScreen class is responsible for synchronizing the combat state".

The system has a 120-token energy budget per session — every word counts.

### What makes a valuable lesson

A lesson must help a FUTURE agent working on this codebase. It is stored in a memory graph and recalled via BM25 token matching. Ask: "if an agent matches this atom in 3 months, does it save them from a mistake, answer a question, or reveal something non-obvious?"

DO NOT write lessons that merely describe what code changed. Git log and the code itself capture that. These have zero future recall value:
- BAD: "dm.earned_cards was renamed to _reward_earned_cards" (git blame shows this)
- BAD: "function_X was added to file_Y" (the code shows this)
- BAD: "bug was fixed in file_Z" (the commit message says this)

Instead write lessons that capture WHY, WHEN, or HOW — things the code alone doesn't tell you:
- GOOD: "frigate cargo_bays 1" (answers a domain question without reading code)
- GOOD: "elite_card_reward tracks via _reward_earned_cards not dm" (non-obvious — the old name looks correct)
- GOOD: "slot_defaults can be overridden per slot_type in ship_data" (corrects a wrong assumption)
- GOOD: "dm.earned_cards is deprecated use reward_tracker locals instead" (warns future agents away from a trap)

### Code anchor lessons

When the session touched or discussed key functions, classes, constants, or
scene nodes, extract **code anchor** lessons that map identifiers to their
purpose. Include the identifier name so it's greppable.

Only extract anchors that are greppable entry points (unique names that
would find the right file in one search). Not local variables or generic
helpers. These help future agents locate code without reading whole files.

Code anchors DO NOT count against the 8-lesson maximum. Cap at 4 per session.
Code anchor confidence should be >= 0.85 — they are directly observed facts, not inferences.

Examples:
- GOOD: "_award_xp implements xp_level_progression" (unique, greppable, maps code→concept)
- GOOD: "XP_BASE configures xp_level_curve_base_value" (answers what this constant controls)
- GOOD: "_create_stat_bar_row creates color_coded_stat_bar with dim_background" (captures pattern)
- BAD: "var i iterates loop" (not unique, not useful)
- BAD: "main.gd contains game_logic" (filename, not code identifier)

### Prediction correction lessons

When prediction_reconciliation contains contradictions, you MUST extract correction lessons that fix the wrong prediction. If the system predicted X based on memory M but the actual answer was Y, the high-value lesson is one that corrects or qualifies M so the prediction won't be wrong next time. These correction lessons should have source: "prediction".

Only write lessons if at least one of: corrections exist, task incomplete, peak intensity > 0.3, decisions exist, files touched > 0, new observations not in retrieved memories, or prediction_reconciliation contains contradictions.

Scope: project (codebase-specific) or universal_candidate (cross-project).
Confidence: 0.90+ (developer explicitly stated rule), 0.75-0.90 (observed and accepted), 0.50-0.75 (inferred, not confirmed), below 0.50 (do not write).

prediction_reconciliation — reconcile reflect predictions against what actually happened. For each element (files, approach, outcome, risks): matched, partial, missed, or wrong. Set to null if no prediction exists or if cold_start was true.

Prediction reconciliation shape when present:
{
  "cold_start": false,
  "elements": [
    {
      "element":       "files | approach | outcome | risks",
      "predicted":     "what was predicted",
      "actual":        "what actually happened",
      "result":        "matched | partial | missed | wrong",
      "source_tuples": [],
      "event":         "prediction_confirmed | prediction_contradicted | null",
      "lesson":        "free-form text correction or null"
    }
  ],
  "matched_count": 0,
  "total_count":   0,
  "overall":       "matched | partial | missed",
  "summary":       "one-line summary"
}`

// doLearn runs the learn step: gathers contracts + transcript from the DB,
// calls an LLM to produce a contract:learn>consolidate JSON, and persists it.
func doLearn(sessionID string) (string, error) {
	root, err := store.RepoRoot()
	if err != nil {
		return "", fmt.Errorf("repo root: %w", err)
	}
	s, err := store.Open(root)
	if err != nil {
		return "", fmt.Errorf("open store: %w", err)
	}
	defer s.Close()

	// Read contracts
	senseJSON, err := store.ReadContract(s.DB(), sessionID, "sense")
	if err != nil {
		return "", fmt.Errorf("read sense: %w", err)
	}
	recallJSON, err := store.ReadContract(s.DB(), sessionID, "recall")
	if err != nil {
		return "", fmt.Errorf("read recall: %w", err)
	}
	reflectJSON, _ := store.ReadContract(s.DB(), sessionID, "reflect")
	executeMetaJSON, _ := store.ReadContract(s.DB(), sessionID, "execute_meta")

	// Read transcript
	responses, err := store.ListResponses(s.DB(), sessionID)
	if err != nil {
		return "", fmt.Errorf("read transcript: %w", err)
	}

	// Pre-compute fields from recall + reflect + execute_meta contracts
	// that the LLM would otherwise have to extract manually.
	precomputed := precomputeFields(recallJSON, reflectJSON, executeMetaJSON)

	// Build user prompt with all context
	var userPrompt strings.Builder
	fmt.Fprintf(&userPrompt, "## Sense contract\n\n```json\n%s\n```\n\n", senseJSON)
	fmt.Fprintf(&userPrompt, "## Recall contract\n\n```json\n%s\n```\n\n", recallJSON)
	fmt.Fprintf(&userPrompt, "## Pre-computed fields (copy verbatim, do NOT re-derive)\n\n```json\n%s\n```\n\n", precomputed)
	if reflectJSON != "" {
		fmt.Fprintf(&userPrompt, "## Reflect contract\n\n```json\n%s\n```\n\n", reflectJSON)
	} else {
		fmt.Fprintf(&userPrompt, "## Reflect contract\n\n(none)\n\n")
	}

	fmt.Fprintf(&userPrompt, "## Transcript (%d turns)\n\n", len(responses))
	for _, r := range responses {
		ts := time.Unix(r.CreatedAt, 0).UTC().Format("15:04:05Z")
		if r.Role == "user" {
			fmt.Fprintf(&userPrompt, "### [%s] User\n\n%s\n\n", ts, safeResultText(r))
		} else {
			fmt.Fprintf(&userPrompt, "### [%s] Assistant\n\n%s\n\n", ts, safeResultText(r))
		}
	}

	timestampEnd := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	systemPrompt := strings.ReplaceAll(learnSystemPrompt, "<TIMESTAMP_END>", timestampEnd)

	fmt.Fprintf(os.Stderr, "learning from session %s...\n", sessionID)

	// Call LLM — learn uses its own model config
	raw, err := callLearnLLM(root, systemPrompt, userPrompt.String())
	if err != nil {
		return "", fmt.Errorf("learn LLM: %w", err)
	}

	raw = stripJSONFences(strings.TrimSpace(raw))

	// Validate it's parseable JSON
	var check map[string]any
	if err := json.Unmarshal([]byte(raw), &check); err != nil {
		return "", fmt.Errorf("invalid JSON from learn: %v\nraw: %s", err, raw)
	}

	// Persist learn contract
	if err := store.WriteContract(s.DB(), sessionID, "learn", raw); err != nil {
		fmt.Fprintf(os.Stderr, "heb: session write learn: %v\n", err)
	}

	return raw, nil
}

// precomputeFields extracts fields from recall + reflect contracts so the
// learn LLM can copy them verbatim instead of deriving them (which was
// error-prone — arrays vs strings, miscounts, dropped source_tuples).
func precomputeFields(recallJSON, reflectJSON, executeMetaJSON string) string {
	// Recall: edge tuples and memory counts
	var recall struct {
		Memories []struct {
			Tuple  string `json:"tuple"`
			Source string `json:"source"`
		} `json:"memories"`
		GitRefs []json.RawMessage `json:"git_refs"`
	}
	json.Unmarshal([]byte(recallJSON), &recall)

	var edgeTuples []string
	for _, m := range recall.Memories {
		if m.Source == "edge" {
			edgeTuples = append(edgeTuples, m.Tuple)
		}
	}

	memoriesLoaded := len(recall.Memories)
	gitRefs := len(recall.GitRefs)
	coldStart := memoriesLoaded == 0 && gitRefs == 0

	result := map[string]any{
		"recalled_via_edges": edgeTuples,
		"memory_loaded": map[string]any{
			"memories_loaded": memoriesLoaded,
			"git_refs":        gitRefs,
			"was_cold_start":  coldStart,
		},
	}

	// Reflect: prediction source_tuples per element type
	if reflectJSON != "" {
		var reflect struct {
			Prediction struct {
				Files    []struct{ SourceTuples []string `json:"source_tuples"` } `json:"files"`
				Approach struct{ SourceTuples []string `json:"source_tuples"` }   `json:"approach"`
				Outcome  struct{ SourceTuples []string `json:"source_tuples"` }   `json:"outcome"`
				Risks    []struct{ SourceTuples []string `json:"source_tuples"` }  `json:"risks"`
			} `json:"prediction"`
		}
		if json.Unmarshal([]byte(reflectJSON), &reflect) == nil {
			pst := map[string][]string{
				"approach": reflect.Prediction.Approach.SourceTuples,
				"outcome":  reflect.Prediction.Outcome.SourceTuples,
			}
			// Merge source_tuples from all file predictions
			var fileTuples []string
			for _, f := range reflect.Prediction.Files {
				fileTuples = append(fileTuples, f.SourceTuples...)
			}
			pst["files"] = fileTuples
			// Merge source_tuples from all risk predictions
			var riskTuples []string
			for _, r := range reflect.Prediction.Risks {
				riskTuples = append(riskTuples, r.SourceTuples...)
			}
			pst["risks"] = riskTuples
			result["prediction_source_tuples"] = pst
		}
	}

	// Execute meta: files_touched and files_read from tool_use parsing
	if executeMetaJSON != "" {
		var meta struct {
			FilesTouched []string `json:"files_touched"`
			FilesRead    []string `json:"files_read"`
		}
		if json.Unmarshal([]byte(executeMetaJSON), &meta) == nil {
			if len(meta.FilesTouched) > 0 {
				result["files_touched"] = meta.FilesTouched
			}
			if len(meta.FilesRead) > 0 {
				result["files_read"] = meta.FilesRead
			}
		}
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out)
}

// safeResultText returns the best display text for a transcript entry.
// For assistant turns containing stream-json (NDJSON), it extracts text
// blocks verbatim and emits compact one-line summaries for tool_use blocks
// so the learn LLM can see function names and file paths without full diffs.
func safeResultText(r store.SessionResponse) string {
	// For user turns, response IS the prompt text
	if r.Role == "user" {
		if r.ResultText != nil && *r.ResultText != "" {
			return *r.ResultText
		}
		return r.Response
	}

	// For assistant turns, try to parse stream-json and extract meaningful content
	if strings.Contains(r.Response, `"type"`) && strings.Contains(r.Response, "\n") {
		if extracted := extractTranscriptText(r.Response); extracted != "" {
			return extracted
		}
	}

	// Fallback: use result_text if available
	if r.ResultText != nil && *r.ResultText != "" {
		return *r.ResultText
	}
	if len(r.Response) > 500 {
		return "(full JSON response — " + fmt.Sprintf("%d bytes", len(r.Response)) + ")"
	}
	return r.Response
}

// extractTranscriptText parses stream-json NDJSON and returns a compact
// representation: text blocks verbatim, tool_use blocks as one-line summaries
// showing the tool name, file path, and a snippet of the content being changed.
func extractTranscriptText(raw string) string {
	var parts []string

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var m struct {
			Type    string `json:"type"`
			Result  string `json:"result"`
			Message *struct {
				Content []struct {
					Type       string          `json:"type"`
					Text       string          `json:"text"`
					Name       string          `json:"name"`
					RawContent json.RawMessage `json:"content"` // string for tool_result, absent for tool_use
					Input      struct {
						FilePath  string `json:"file_path"`
						Path      string `json:"path"`
						Command   string `json:"command"`
						Pattern   string `json:"pattern"`
						OldString string `json:"old_string"`
						Content   string `json:"content"`
					} `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}

		if m.Type == "result" && m.Result != "" {
			parts = append(parts, m.Result)
			continue
		}

		if m.Message == nil {
			continue
		}

		for _, c := range m.Message.Content {
			switch c.Type {
			case "text":
				if c.Text != "" {
					parts = append(parts, c.Text)
				}
			case "tool_use":
				parts = append(parts, formatToolSummary(c.Name, c.Input.FilePath, c.Input.Path, c.Input.Command, c.Input.Pattern, c.Input.OldString, c.Input.Content))
			case "tool_result":
				if text := extractToolResultText(c.RawContent); text != "" {
					parts = append(parts, formatToolResult(text))
				}
			}
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// formatToolSummary produces a compact one-line summary of a tool_use block.
// For Edit: shows file path and first line of old_string (contains function signatures).
// For Write: shows file path and first line of content.
// For Read/Grep/Glob: shows the target path/pattern.
// For Bash: shows the command.
func formatToolSummary(name, filePath, path, command, pattern, oldString, content string) string {
	switch name {
	case "Edit":
		snippet := firstLine(oldString, 80)
		if snippet != "" {
			return fmt.Sprintf("[Edit: %s — %s]", filePath, snippet)
		}
		return fmt.Sprintf("[Edit: %s]", filePath)
	case "Write":
		snippet := firstLine(content, 80)
		if snippet != "" {
			return fmt.Sprintf("[Write: %s — %s]", filePath, snippet)
		}
		return fmt.Sprintf("[Write: %s]", filePath)
	case "Read":
		return fmt.Sprintf("[Read: %s]", filePath)
	case "Grep":
		p := path
		if p == "" {
			p = filePath
		}
		return fmt.Sprintf("[Grep: %q in %s]", pattern, p)
	case "Glob":
		return fmt.Sprintf("[Glob: %s]", pattern)
	case "Bash":
		return fmt.Sprintf("[Bash: %s]", firstLine(command, 120))
	default:
		if filePath != "" {
			return fmt.Sprintf("[%s: %s]", name, filePath)
		}
		return fmt.Sprintf("[%s]", name)
	}
}

// extractToolResultText extracts text from a tool_result content field,
// which can be a JSON string or an array of content blocks [{type:"text",text:"..."}].
func extractToolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try as string first
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	// Try as array of content blocks
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var texts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				texts = append(texts, b.Text)
			}
		}
		return strings.Join(texts, "\n")
	}
	return ""
}

// formatToolResult produces a compact summary of a tool_result block.
// Short results (errors, small outputs) are shown in full. Long results
// (file contents, large grep output) are summarized as line count + first
// few significant lines to capture function signatures and identifiers.
func formatToolResult(content string) string {
	lines := strings.Split(content, "\n")
	// Short results: show in full (likely errors or confirmations)
	if len(lines) <= 5 {
		return fmt.Sprintf("[Result: %s]", strings.TrimSpace(content))
	}
	// Long results: show line count + first few non-empty lines
	// that look like function/class/const definitions
	var sigLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Prioritise lines with identifiers: func, class, const, def, var, export, etc.
		if isSignatureLine(trimmed) {
			sigLines = append(sigLines, trimmed)
			if len(sigLines) >= 4 {
				break
			}
		}
	}
	if len(sigLines) > 0 {
		return fmt.Sprintf("[Result: %d lines — %s]", len(lines), strings.Join(sigLines, "; "))
	}
	// Fallback: first 3 non-empty lines
	var preview []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			preview = append(preview, trimmed)
			if len(preview) >= 3 {
				break
			}
		}
	}
	return fmt.Sprintf("[Result: %d lines — %s]", len(lines), strings.Join(preview, "; "))
}

// isSignatureLine checks if a line looks like a function/class/constant
// definition that would be useful as a code anchor identifier.
func isSignatureLine(line string) bool {
	prefixes := []string{
		"func ", "def ", "class ", "const ", "var ", "let ", "export ",
		"type ", "interface ", "enum ", "struct ", "fn ",
		"public ", "private ", "protected ", "static ",
		"signal ", "onready ",
	}
	lower := strings.ToLower(line)
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// firstLine returns the first non-empty line of s, truncated to maxLen.
func firstLine(s string, maxLen int) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len(line) > maxLen {
				return line[:maxLen] + "..."
			}
			return line
		}
	}
	return ""
}

// callLearnLLM calls the configured LLM for the learn step.
// Hardcoded to OpenAI o3-mini for now — reasoning model that handles
// structured JSON well and doesn't require spawning a claude session.
func callLearnLLM(repoRoot, systemPrompt, userPrompt string) (string, error) {
	_, apiKey := resolveProvider()
	if apiKey == "" {
		// Fall back to env directly
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey != "" {
		return learnViaOpenAI(apiKey, "gpt-4.1", systemPrompt, userPrompt)
	}

	// Last resort: claude --print (will fail inside Claude Code)
	return callLearnViaClaude("", systemPrompt, userPrompt)
}

// learnViaOpenAI calls the OpenAI API with a model suitable for learn.
// Handles both reasoning models (o-series: developer role, max_completion_tokens)
// and standard models (system role, max_tokens).
func learnViaOpenAI(apiKey, model, systemPrompt, userPrompt string) (string, error) {
	isReasoning := strings.HasPrefix(model, "o")

	var messages []map[string]string
	if isReasoning {
		messages = []map[string]string{
			{"role": "developer", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		}
	} else {
		messages = []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		}
	}

	reqBody := map[string]any{
		"model":    model,
		"messages": messages,
	}
	if isReasoning {
		reqBody["max_completion_tokens"] = 16384
	} else {
		reqBody["max_tokens"] = 8192
		reqBody["temperature"] = 0
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("api call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("openai api returned %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("empty response from openai")
	}

	return apiResp.Choices[0].Message.Content, nil
}

// resolveAnthropicModel maps short names to full model IDs.
func resolveAnthropicModel(name string) string {
	switch strings.ToLower(name) {
	case "opus":
		return "claude-opus-4-6"
	case "sonnet":
		return "claude-sonnet-4-6"
	case "haiku":
		return "claude-haiku-4-5-20251001"
	default:
		return name
	}
}

// learnViaAnthropic calls the Anthropic Messages API with a configurable model.
func learnViaAnthropic(apiKey, model, systemPrompt, userPrompt string) (string, error) {
	reqBody := map[string]any{
		"model":      model,
		"max_tokens": 4096,
		"system":     systemPrompt,
		"messages": []map[string]string{
			{"role": "user", "content": userPrompt},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Anthropic-Version", "2023-06-01")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("api call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("api returned %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(apiResp.Content) == 0 {
		return "", fmt.Errorf("empty response from api")
	}

	return apiResp.Content[0].Text, nil
}

// callLearnViaClaude uses claude --print with optional --model for the learn step.
func callLearnViaClaude(model, systemPrompt, userPrompt string) (string, error) {
	cwd, _ := os.Getwd()

	claudeArgs := []string{
		"--print",
		"--no-session-persistence",
		"--system-prompt", systemPrompt,
		"--allowedTools", "",
	}
	if model != "" {
		claudeArgs = append(claudeArgs, "--model", model)
	}
	claudeArgs = append(claudeArgs, "-p", userPrompt)

	cmd := exec.Command("claude", claudeArgs...)
	cmd.Dir = cwd
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")
	cmd.Stderr = os.Stderr

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude --print: %w", err)
	}

	return stdout.String(), nil
}

// runLearn is the `heb learn` entry point.
func runLearn(args []string) int {
	root, err := store.RepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: %v\n", err)
		return 1
	}
	s, err := store.Open(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: %v\n", err)
		return 1
	}
	defer s.Close()

	var sessionID string
	if len(args) > 0 {
		sessionID = args[0]
	} else {
		session, err := store.LatestActiveSession(s.DB())
		if err != nil {
			fmt.Fprintf(os.Stderr, "heb: %v\n", err)
			return 1
		}
		sessionID = session.ID
	}

	raw, err := doLearn(sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: %v\n", err)
		return 1
	}

	printLearnSummary(raw, "learned")

	// JSON to stdout
	fmt.Fprintln(os.Stdout, raw)
	return 0
}

// runRemember is the `heb remember` entry point: learn then consolidate then commit then close.
func runRemember(args []string) int {
	// Parse --no-commit flag
	doCommit := true
	var filtered []string
	for _, a := range args {
		if a == "--no-commit" {
			doCommit = false
		} else {
			filtered = append(filtered, a)
		}
	}
	args = filtered

	root, err := store.RepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: %v\n", err)
		return 1
	}
	s, err := store.Open(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: %v\n", err)
		return 1
	}
	defer s.Close()

	var sessionID string
	if len(args) > 0 {
		sessionID = args[0]
	} else {
		session, err := store.LatestActiveSession(s.DB())
		if err != nil {
			fmt.Fprintf(os.Stderr, "heb: %v\n", err)
			return 1
		}
		sessionID = session.ID
	}

	// Step 1: Learn
	learnJSON, err := doLearn(sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: learn: %v\n", err)
		return 1
	}

	// Step 2: Git commit (before consolidation mutates .heb/)
	if doCommit {
		if err := commitSessionWork(root, learnJSON, 0); err != nil {
			fmt.Fprintf(os.Stderr, "heb: commit: %v\n", err)
			// Non-fatal — continue with consolidation
		}
	}

	// Step 3: Consolidate (call directly, no subprocess)
	fmt.Fprintln(os.Stderr, "consolidating...")
	cfg := consolidate.DefaultConfig()
	cfg.Format = "json"

	var lr consolidate.LearnResult
	if err := json.Unmarshal([]byte(learnJSON), &lr); err != nil {
		fmt.Fprintf(os.Stderr, "heb: parse learn contract: %v\n", err)
		return 1
	}
	lr.Raw = append(json.RawMessage(nil), []byte(learnJSON)...)

	if lr.Project == "" {
		fmt.Fprintln(os.Stderr, "heb: consolidate: project required")
		return 1
	}

	result := consolidate.Run(lr, cfg)
	if err := applyPayload(&result, &lr, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "heb: consolidate: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, consolidate.StderrSummary(result))

	// Step 4: Close session
	if err := store.CloseSession(s.DB(), sessionID); err != nil {
		fmt.Fprintf(os.Stderr, "heb: close session: %v\n", err)
		// Non-fatal — consolidation already succeeded
	} else {
		fmt.Fprintf(os.Stderr, "session closed: %s\n", sessionID)
	}

	printLearnSummary(learnJSON, "remembered")
	return 0
}

// commitSessionWork stages files from the session and commits with an LLM-generated message.
func commitSessionWork(repoRoot, learnJSON string, lessonsWritten int) error {
	var learn struct {
		RawPrompt string `json:"raw_prompt"`
		Lessons   []struct {
			Body        string `json:"body"`
			Observation string `json:"observation"` // backward compat
		} `json:"lessons"`
		Corrections []struct {
			What       string `json:"what"`
			Correction string `json:"correction"`
		} `json:"corrections"`
		Implementation struct {
			FilesTouched []string `json:"files_touched"`
			Approach     string   `json:"approach"`
		} `json:"implementation"`
	}
	json.Unmarshal([]byte(learnJSON), &learn)

	// Collect files to stage: implementation files + .heb/ database
	var filesToStage []string
	for _, f := range learn.Implementation.FilesTouched {
		filesToStage = append(filesToStage, f)
	}
	filesToStage = append(filesToStage, ".heb/")

	// Check if there's anything to commit
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = repoRoot
	statusOut, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if len(strings.TrimSpace(string(statusOut))) == 0 {
		fmt.Fprintln(os.Stderr, "nothing to commit")
		return nil
	}

	// Stage files
	stageArgs := append([]string{"add", "--"}, filesToStage...)
	stageCmd := exec.Command("git", stageArgs...)
	stageCmd.Dir = repoRoot
	stageCmd.Stderr = os.Stderr
	if err := stageCmd.Run(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	// Check if anything was actually staged
	diffCmd := exec.Command("git", "diff", "--cached", "--quiet")
	diffCmd.Dir = repoRoot
	if diffCmd.Run() == nil {
		fmt.Fprintln(os.Stderr, "nothing staged to commit")
		return nil
	}

	// Generate commit message via LLM
	commitMsg, err := generateCommitMessage(repoRoot, learn.RawPrompt, learn.Implementation.Approach, learn.Implementation.FilesTouched, len(learn.Lessons), lessonsWritten)
	if err != nil {
		// Fallback to a generic message
		commitMsg = fmt.Sprintf("heb: %s", learn.Implementation.Approach)
	}
	commitMsg += fmt.Sprintf("\n\nMemories: %d learned, %d consolidated\n\nCo-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>", len(learn.Lessons), lessonsWritten)

	// Commit
	commitCmd := exec.Command("git", "commit", "-m", commitMsg)
	commitCmd.Dir = repoRoot
	commitCmd.Stderr = os.Stderr
	commitCmd.Stdout = os.Stderr
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	fmt.Fprintln(os.Stderr, "committed")
	return nil
}

const commitMsgSystemPrompt = `You write git commit messages. Output ONLY the commit subject line (max 72 chars). Use conventional commit format (feat:, fix:, refactor:, docs:, chore:, etc.). Describe the actual work, not the pipeline. No quotes, no explanation.`

// generateCommitMessage calls a fast LLM to produce a one-line commit subject.
func generateCommitMessage(repoRoot, rawPrompt, approach string, filesTouched []string, lessonCount, consolidatedCount int) (string, error) {
	userPrompt := fmt.Sprintf("Prompt: %s\nApproach: %s\nFiles: %s\nLessons: %d",
		rawPrompt, approach, strings.Join(filesTouched, ", "), lessonCount)

	_, apiKey := resolveProvider()
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	var msg string
	var err error

	if apiKey != "" {
		msg, err = learnViaOpenAI(apiKey, "gpt-4.1-mini", commitMsgSystemPrompt, userPrompt)
	} else {
		msg, err = callLearnViaClaude("haiku", commitMsgSystemPrompt, userPrompt)
	}

	if err != nil {
		return "", err
	}

	// Clean up — take first line only, trim whitespace
	msg = strings.TrimSpace(msg)
	if idx := strings.IndexByte(msg, '\n'); idx >= 0 {
		msg = msg[:idx]
	}
	return msg, nil
}

// printLearnSummary prints a human-readable summary of the learn contract to stderr.
func printLearnSummary(learnJSON, verb string) {
	var learn struct {
		Lessons []struct {
			Body        string  `json:"body"`
			Observation string  `json:"observation"` // backward compat
			Scope       string  `json:"scope"`
			Confidence  float64 `json:"confidence"`
		} `json:"lessons"`
		Corrections []struct {
			What       string  `json:"what"`
			Correction string  `json:"correction"`
			Intensity  float64 `json:"intensity"`
		} `json:"corrections"`
		Completed bool `json:"completed"`
		Implementation struct {
			FilesTouched []string `json:"files_touched"`
			Approach     string   `json:"approach"`
		} `json:"implementation"`
	}
	json.Unmarshal([]byte(learnJSON), &learn)

	fmt.Fprintf(os.Stderr, "%s: %d lessons, %d corrections\n", verb, len(learn.Lessons), len(learn.Corrections))

	if learn.Implementation.Approach != "" {
		fmt.Fprintf(os.Stderr, "  approach: %s\n", learn.Implementation.Approach)
	}
	if len(learn.Implementation.FilesTouched) > 0 {
		fmt.Fprintf(os.Stderr, "  files:    %s\n", strings.Join(learn.Implementation.FilesTouched, ", "))
	}

	for _, l := range learn.Lessons {
		body := l.Body
		if body == "" {
			body = l.Observation // backward compat
		}
		fmt.Fprintf(os.Stderr, "  + %s  [%s %.2f]\n", body, l.Scope, l.Confidence)
	}
	for _, c := range learn.Corrections {
		fmt.Fprintf(os.Stderr, "  ! %s → %s  [%.1f]\n", c.What, c.Correction, c.Intensity)
	}

	if !learn.Completed {
		fmt.Fprintln(os.Stderr, "  (task was not completed)")
	}
}
