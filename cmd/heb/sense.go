package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steelboltgames/heb/internal/store"
)

// senseSystemPrompt is the embedded sense contract.
// The LLM acts as a pure parser — no tools, no file reads, just structured output.
const senseSystemPrompt = `You are a pure parser. Your only job is to turn the raw prompt into a structured JSON object.

## Hard rules — do not violate

- DO NOT read files, run commands, or call any tools
- DO NOT try to solve, suggest fixes, or comment on the task
- DO NOT add fields, omit fields, or reshape the contract
- Same input must always produce the same output
- Complete in a single response, no follow-ups
- Output ONLY the JSON object — no markdown fences, no preamble, no commentary

## Output shape

{
  "session_id":  "<SESSION_ID>",
  "project":     "<PROJECT>",
  "tokens":      [],
  "raw":         "original prompt verbatim"
}

## Field rules

### session_id
Always use: <SESSION_ID>

### project
Always use: <PROJECT>

### tokens

Extract the meaningful words from the prompt — the words that would match
something in a memory graph. Fix obvious typos before extracting (but never
modify the "raw" field).

Preserve identifiers exactly as written: MyProfile, PlayerController,
drone_I. Join adjacent words that form a single concept with underscores:
drone stats → drone_stats, player movement → player_movement.

### raw
Original prompt verbatim. Never modified.`

// senseResult is the parsed sense contract output.
type senseResult struct {
	SessionID string   `json:"session_id"`
	Project   string   `json:"project"`
	Tokens    []string `json:"tokens"`
	Raw       string   `json:"raw"`
}

// doSense runs the sense step: calls the LLM, parses the result,
// persists to session, and returns the sense result and its JSON.
// Prints diagnostics to stderr.
func doSense(prompt string) (*senseResult, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("getwd: %w", err)
	}
	project := filepath.Base(cwd)
	sessionID := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	systemPrompt := senseSystemPrompt
	systemPrompt = strings.ReplaceAll(systemPrompt, "<PROJECT>", project)
	systemPrompt = strings.ReplaceAll(systemPrompt, "<SESSION_ID>", sessionID)

	fmt.Fprintf(os.Stderr, "sensing: %s\n", prompt)

	// Call LLM: config > env var > claude --print
	var raw string
	provider, apiKey := resolveProvider()
	switch provider {
	case "anthropic":
		raw, err = senseViaAnthropic(apiKey, systemPrompt, prompt)
	case "openai":
		raw, err = senseViaOpenAI(apiKey, systemPrompt, prompt)
	default:
		raw, err = senseViaClaude(cwd, systemPrompt, prompt)
	}
	if err != nil {
		if strings.Contains(err.Error(), "credit balance") || strings.Contains(err.Error(), "billing") {
			if provider == "openai" {
				fmt.Fprintln(os.Stderr, "hint: check billing at https://platform.openai.com/settings/organization/billing")
			} else {
				fmt.Fprintln(os.Stderr, "hint: add credits at https://console.anthropic.com/settings/billing")
			}
		}
		return nil, "", fmt.Errorf("sense: %w", err)
	}

	raw = stripJSONFences(strings.TrimSpace(raw))

	var sense senseResult
	if err := json.Unmarshal([]byte(raw), &sense); err != nil {
		return nil, "", fmt.Errorf("invalid JSON from sense: %v\nraw: %s", err, raw)
	}

	// Enforce session_id and project from Go (don't trust the LLM)
	sense.SessionID = sessionID
	sense.Project = project
	corrected, _ := json.Marshal(sense)
	raw = string(corrected)

	// Display
	fmt.Fprintln(os.Stderr, "SENSE RESULT")
	fmt.Fprintln(os.Stderr, "────────────────────────────────────────")
	fmt.Fprintf(os.Stderr, "session_id:  %s\n", sense.SessionID)
	fmt.Fprintf(os.Stderr, "project:     %s\n", sense.Project)
	tokens := "—"
	if len(sense.Tokens) > 0 {
		tokens = strings.Join(sense.Tokens, ", ")
	}
	fmt.Fprintf(os.Stderr, "tokens:      %s\n", tokens)
	fmt.Fprintf(os.Stderr, "raw:         %q\n", sense.Raw)
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
			if err := store.StartSession(s.DB(), sense.SessionID, sense.Project, raw); err != nil {
				fmt.Fprintf(os.Stderr, "heb: session start: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "session started: %s\n", sense.SessionID)
			}
		}
	}

	return &sense, raw, nil
}

// runSense is the `heb sense` entry point.
func runSense(args []string) int {
	prompt := strings.Join(args, " ")
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "usage: heb sense <prompt>")
		return 2
	}

	_, raw, err := doSense(prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: %v\n", err)
		return 1
	}

	fmt.Fprintln(os.Stdout, raw)
	return 0
}
