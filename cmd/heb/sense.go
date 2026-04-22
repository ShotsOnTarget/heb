package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/steelboltgames/heb/internal/store"
)

// senseSystemPrompt is the sense contract, loaded from prompts/sense.txt.
// The LLM acts as a pure parser — no tools, no file reads, just structured output.
//
//go:embed prompts/sense.txt
var senseSystemPrompt string

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
	project, err := store.ProjectID()
	if err != nil {
		return nil, "", fmt.Errorf("project id: %w", err)
	}
	sessionID := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	systemPrompt := senseSystemPrompt
	systemPrompt = strings.ReplaceAll(systemPrompt, "<PROJECT>", project)
	systemPrompt = strings.ReplaceAll(systemPrompt, "<SESSION_ID>", sessionID)

	// Call LLM: honor sense.model config, then fall back to auto-detected provider.
	var raw string
	var usage apiUsage
	provider, apiKey, model := resolveModel("sense")
	start := time.Now()
	switch provider {
	case "anthropic":
		fmt.Fprintf(os.Stderr, "sensing [%s]: %s\n", modelLabel(model, defaultAnthropicModel), prompt)
		raw, usage, err = senseViaAnthropic(apiKey, model, systemPrompt, prompt, 0)
	case "openai":
		fmt.Fprintf(os.Stderr, "sensing [%s]: %s\n", modelLabel(model, defaultOpenAIModel), prompt)
		raw, usage, err = senseViaOpenAI(apiKey, model, systemPrompt, prompt, 0)
	default:
		fmt.Fprintf(os.Stderr, "sensing [%s]: %s\n", modelLabel(model, "claude"), prompt)
		raw, usage, err = senseViaClaude(cwd, model, systemPrompt, prompt)
	}
	elapsed := time.Since(start)
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

	// Display tokens on the sense line so the GUI can show them
	tokens := "—"
	if len(sense.Tokens) > 0 {
		tokens = strings.Join(sense.Tokens, ", ")
	}
	fmt.Fprintf(os.Stderr, "sensing tokens: %s\n", tokens)
	emitStats("sense", usage, elapsed)

	// Persist to session (best-effort)
	s, err := store.OpenOrInit()
	if err == nil {
		defer s.Close()
		if err := store.StartSession(s.DB(), sense.SessionID, sense.Project, raw); err != nil {
			fmt.Fprintf(os.Stderr, "heb: session start: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "session started: %s\n", sense.SessionID)
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
