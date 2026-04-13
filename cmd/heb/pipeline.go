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

	"github.com/steelboltgames/heb/internal/store"
)

func runPipeline(args []string) int {
	prompt := strings.Join(args, " ")
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "usage: heb <prompt>")
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

	_, reflectJSON, err := doReflect(sense, ret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: %v\n", err)
		return 1
	}

	// Hand off to claude for execution
	result, err := executeViaClaude(sense.Raw, reflectJSON)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: %v\n", err)
		return 1
	}

	// Print the result text to stdout
	if result.ResultText != "" {
		fmt.Fprint(os.Stdout, result.ResultText)
		if !strings.HasSuffix(result.ResultText, "\n") {
			fmt.Fprintln(os.Stdout)
		}
	}

	// Persist transcript (best-effort)
	root, err := store.RepoRoot()
	if err == nil {
		s, err := store.Open(root)
		if err == nil {
			defer s.Close()
			// Write user prompt turn
			store.WriteUserPrompt(s.DB(), sense.SessionID, sense.Raw)
			// Write assistant response turn
			var claudeID *string
			if result.ClaudeSessionID != "" {
				claudeID = &result.ClaudeSessionID
			}
			var resultText *string
			if result.ResultText != "" {
				resultText = &result.ResultText
			}
			var costUSD *float64
			if result.CostUSD > 0 {
				costUSD = &result.CostUSD
			}
			var numTurns *int
			if result.NumTurns > 0 {
				numTurns = &result.NumTurns
			}
			_, writeErr := store.WriteAssistantResponse(s.DB(), sense.SessionID, claudeID, result.FullJSON, resultText, costUSD, numTurns)
			if writeErr != nil {
				fmt.Fprintf(os.Stderr, "heb: store response: %v\n", writeErr)
			} else {
				fmt.Fprintf(os.Stderr, "transcript stored: claude_session=%s\n", result.ClaudeSessionID)
			}
		}
	}

	return 0
}

// resolveProvider returns the provider name and API key to use.
// Uses the config cascade: project → global → env → default.
// Returns ("", "") if no key is configured — caller falls back to claude --print.
func resolveProvider() (string, string) {
	provider, _ := configLookup("provider", false)
	anthropicKey, _ := configLookup("anthropic-key", false)
	openaiKey, _ := configLookup("openai-key", false)

	// If provider is explicitly set, use it if key exists
	switch provider {
	case "openai":
		if openaiKey != "" {
			return "openai", openaiKey
		}
	case "anthropic":
		if anthropicKey != "" {
			return "anthropic", anthropicKey
		}
	}

	// Auto-detect: use whichever key is available
	if anthropicKey != "" {
		return "anthropic", anthropicKey
	}
	if openaiKey != "" {
		return "openai", openaiKey
	}

	return "", ""
}

// senseViaAnthropic calls the Anthropic Messages API directly with Haiku.
func senseViaAnthropic(apiKey, systemPrompt, userPrompt string) (string, error) {
	reqBody := map[string]any{
		"model":      "claude-haiku-4-5-20251001",
		"max_tokens": 512,
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

	client := &http.Client{Timeout: 30 * time.Second}
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

// senseViaOpenAI calls the OpenAI Chat Completions API with gpt-4.1-mini.
func senseViaOpenAI(apiKey, systemPrompt, userPrompt string) (string, error) {
	reqBody := map[string]any{
		"model":       "gpt-4.1-mini",
		"max_tokens":  1024,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
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

	client := &http.Client{Timeout: 30 * time.Second}
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
		return "", fmt.Errorf("empty response from api")
	}

	return apiResp.Choices[0].Message.Content, nil
}

// executeResult holds the parsed output from a claude --print --output-format json run.
type executeResult struct {
	ClaudeSessionID string  `json:"claude_session_id"`
	ResultText      string  `json:"result_text"`
	CostUSD         float64 `json:"cost_usd"`
	NumTurns        int     `json:"num_turns"`
	FullJSON        string  `json:"full_json"` // raw JSON response
}

// executeViaClaude hands the user's prompt to claude --print --output-format json
// and captures the session ID, transcript, and result.
func executeViaClaude(userPrompt, reflectJSON string) (*executeResult, error) {
	combined := fmt.Sprintf("%s\n\n## Memory context\n\n%s", userPrompt, reflectJSON)
	return runClaudePrint([]string{"--print", "--output-format", "json", "--allowedTools", "Read,Write,Edit,Grep,Glob,Bash"}, combined)
}

// resumeViaClaude resumes an existing Claude session with a new prompt.
func resumeViaClaude(claudeSessionID, userPrompt string) (*executeResult, error) {
	return runClaudePrint([]string{"--print", "--output-format", "json", "--allowedTools", "Read,Write,Edit,Grep,Glob,Bash", "--resume", claudeSessionID}, userPrompt)
}

// runClaude executes claude with the given args and stdin, parsing the JSON response.
func runClaudePrint(claudeArgs []string, stdin string) (*executeResult, error) {
	cwd, _ := os.Getwd()

	cmd := exec.Command("claude", claudeArgs...)
	cmd.Dir = cwd
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stderr = os.Stderr

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	fmt.Fprintln(os.Stderr, "executing...")

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("claude: %w", err)
	}

	return parseClaudeJSON(stdout.String()), nil
}

// parseClaudeJSON extracts session ID, result text, cost, and turns from
// claude --output-format json output.
func parseClaudeJSON(raw string) *executeResult {
	result := &executeResult{FullJSON: raw}

	var messages []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &messages); err != nil {
		var single map[string]any
		if err2 := json.Unmarshal([]byte(raw), &single); err2 == nil {
			messages = []json.RawMessage{json.RawMessage(raw)}
		} else {
			result.ResultText = raw
			return result
		}
	}

	for _, msg := range messages {
		var m struct {
			Type      string  `json:"type"`
			SessionID string  `json:"session_id"`
			Result    string  `json:"result"`
			CostUSD   float64 `json:"total_cost_usd"`
			NumTurns  int     `json:"num_turns"`
			Message   *struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(msg, &m); err != nil {
			continue
		}

		if m.SessionID != "" {
			result.ClaudeSessionID = m.SessionID
		}

		if m.Type == "result" {
			result.ResultText = m.Result
			result.CostUSD = m.CostUSD
			result.NumTurns = m.NumTurns
		} else if m.Type == "assistant" && m.Message != nil {
			for _, c := range m.Message.Content {
				if c.Type == "text" && result.ResultText == "" {
					result.ResultText = c.Text
				}
			}
		}
	}

	return result
}

// runResume continues an open heb session by resuming the Claude conversation.
// Usage: heb resume [session_id] <prompt...>
func runResume(args []string) int {
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
	var prompt string

	// If first arg looks like a session ID (ISO8601), use it; otherwise pick latest active.
	if len(args) > 0 && len(args[0]) >= 20 && strings.Contains(args[0], "T") && strings.Contains(args[0], "Z") {
		sessionID = args[0]
		prompt = strings.Join(args[1:], " ")
	} else {
		session, err := store.LatestActiveSession(s.DB())
		if err != nil {
			fmt.Fprintf(os.Stderr, "heb: %v\n", err)
			return 1
		}
		sessionID = session.ID
		prompt = strings.Join(args, " ")
	}

	if prompt == "" {
		fmt.Fprintln(os.Stderr, "usage: heb resume [session_id] <prompt>")
		return 2
	}

	// Find the Claude session to resume
	claudeSessionID, err := store.LatestClaudeSessionID(s.DB(), sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: no claude session to resume for %s: %v\n", sessionID, err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "resuming session %s (claude: %s)\n", sessionID, claudeSessionID)

	// Record user prompt
	store.WriteUserPrompt(s.DB(), sessionID, prompt)

	// Resume Claude
	result, err := resumeViaClaude(claudeSessionID, prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: %v\n", err)
		return 1
	}

	// Print result
	if result.ResultText != "" {
		fmt.Fprint(os.Stdout, result.ResultText)
		if !strings.HasSuffix(result.ResultText, "\n") {
			fmt.Fprintln(os.Stdout)
		}
	}

	// Store assistant response
	var claudeID *string
	if result.ClaudeSessionID != "" {
		claudeID = &result.ClaudeSessionID
	}
	var resultText *string
	if result.ResultText != "" {
		resultText = &result.ResultText
	}
	var costUSD *float64
	if result.CostUSD > 0 {
		costUSD = &result.CostUSD
	}
	var numTurns *int
	if result.NumTurns > 0 {
		numTurns = &result.NumTurns
	}
	_, writeErr := store.WriteAssistantResponse(s.DB(), sessionID, claudeID, result.FullJSON, resultText, costUSD, numTurns)
	if writeErr != nil {
		fmt.Fprintf(os.Stderr, "heb: store response: %v\n", writeErr)
	} else {
		fmt.Fprintf(os.Stderr, "transcript stored: claude_session=%s\n", result.ClaudeSessionID)
	}

	return 0
}

// senseViaClaude falls back to spawning claude --print.
func senseViaClaude(cwd, systemPrompt, userPrompt string) (string, error) {
	claudeArgs := []string{
		"--print",
		"--no-session-persistence",
		"--system-prompt", systemPrompt,
		"--allowedTools", "",
		"-p", userPrompt,
	}

	cmd := exec.Command("claude", claudeArgs...)
	cmd.Dir = cwd
	cmd.Stderr = os.Stderr

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude --print: %w", err)
	}

	return stdout.String(), nil
}

// stripJSONFences removes ```json ... ``` wrapping if present.
func stripJSONFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return s
}

// filterEnv returns a copy of env with the named variable removed.
func filterEnv(env []string, name string) []string {
	prefix := name + "="
	out := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}
