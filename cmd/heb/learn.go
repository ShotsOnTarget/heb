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
  "corrections": [],
  "correction_count": 0,
  "peak_intensity":   0.0,
  "completed":        true,

  "lessons": [
    {
      "observation": "subject·predicate·object",
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

memory_loaded — from recall contract: memories_loaded is the count of memories array, git_refs is the count of git_refs array, was_cold_start is true if both are zero.
recalled_via_edges — from recall contract, tuples where source is "edge". Each element is a flat "subject·predicate·object" string, NOT a nested array. Emit ["a·b·c"] not [["a","b","c"]].

implementation.files_read — every file the agent opened, read, searched, or grep'd during execution. Deduplicate alphabetically. Repo-relative paths.
implementation.files_touched — files the agent actually edited or created. Subset of files_read.
implementation.surprise_touches — files in files_read with no obvious connection to the tokens. Empty if intent was understand.
implementation.approach — one sentence, past tense, concrete.
implementation.patterns_used — architectural patterns actually applied. Empty if none.

decisions — every question the agent asked AND the developer answered. Weight: high (design decision), medium (clarification), low (confirmation). Empty if the agent asked nothing.

corrections — every developer correction. Intensity: 0.1-0.3 (minor), 0.4-0.6 (clear), 0.7-0.8 (emphatic), 0.9-1.0 (hard/caps/repetition). Empty if none.

correction_count — length of corrections array.
peak_intensity — max intensity, or 0.0.
completed — true if the task was finished.

lessons — what should be remembered. Tuple format: subject·predicate·object. Max 8. Min 0. Confidence >= 0.50.

Only write lessons if at least one of: corrections exist, task incomplete, peak intensity > 0.3, decisions exist, files touched > 0, or new observations not in retrieved memories.

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
      "lesson":        "subject·predicate·object or null"
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

	// Read transcript
	responses, err := store.ListResponses(s.DB(), sessionID)
	if err != nil {
		return "", fmt.Errorf("read transcript: %w", err)
	}

	// Build user prompt with all context
	var userPrompt strings.Builder
	fmt.Fprintf(&userPrompt, "## Sense contract\n\n```json\n%s\n```\n\n", senseJSON)
	fmt.Fprintf(&userPrompt, "## Recall contract\n\n```json\n%s\n```\n\n", recallJSON)
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

// safeResultText returns the best display text for a transcript entry.
func safeResultText(r store.SessionResponse) string {
	if r.ResultText != nil && *r.ResultText != "" {
		return *r.ResultText
	}
	// For user turns, response IS the prompt text
	if r.Role == "user" {
		return r.Response
	}
	// For assistant turns with full JSON, try to extract result
	if len(r.Response) > 500 {
		return "(full JSON response — " + fmt.Sprintf("%d bytes", len(r.Response)) + ")"
	}
	return r.Response
}

// callLearnLLM calls the configured LLM for the learn step.
// Uses learn.model config if set, otherwise falls back to default provider.
// If no API key, falls back to claude --print with optional --model.
func callLearnLLM(repoRoot, systemPrompt, userPrompt string) (string, error) {
	s, err := store.Open(repoRoot)
	if err != nil {
		return callLearnViaClaude("", systemPrompt, userPrompt)
	}
	defer s.Close()

	learnModel, _ := store.ConfigGet(s.DB(), "learn.model")

	// If learn.model is set, always use claude --print --model <model>.
	// This ensures "opus" or "sonnet" works regardless of which API keys exist.
	if learnModel != "" {
		return callLearnViaClaude(learnModel, systemPrompt, userPrompt)
	}

	// No learn.model set — use whatever API key is available.
	provider, apiKey := resolveProvider()
	switch provider {
	case "anthropic":
		return learnViaAnthropic(apiKey, "claude-haiku-4-5-20251001", systemPrompt, userPrompt)
	case "openai":
		return senseViaOpenAI(apiKey, systemPrompt, userPrompt)
	}

	return callLearnViaClaude("", systemPrompt, userPrompt)
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

	// Step 2: Consolidate (call directly, no subprocess)
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

	// Step 3: Git commit
	if doCommit {
		if err := commitSessionWork(root, learnJSON, len(result.Applied)); err != nil {
			fmt.Fprintf(os.Stderr, "heb: commit: %v\n", err)
			// Non-fatal — consolidation already succeeded
		}
	}

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
			Observation string `json:"observation"`
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

	provider, apiKey := resolveProvider()
	var msg string
	var err error

	switch provider {
	case "anthropic":
		msg, err = learnViaAnthropic(apiKey, "claude-haiku-4-5-20251001", commitMsgSystemPrompt, userPrompt)
	case "openai":
		msg, err = senseViaOpenAI(apiKey, commitMsgSystemPrompt, userPrompt)
	default:
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
			Observation string  `json:"observation"`
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
		fmt.Fprintf(os.Stderr, "  + %s  [%s %.2f]\n", l.Observation, l.Scope, l.Confidence)
	}
	for _, c := range learn.Corrections {
		fmt.Fprintf(os.Stderr, "  ! %s → %s  [%.1f]\n", c.What, c.Correction, c.Intensity)
	}

	if !learn.Completed {
		fmt.Fprintln(os.Stderr, "  (task was not completed)")
	}
}
