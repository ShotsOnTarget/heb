package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/steelboltgames/heb/internal/retrieve"
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

	// Filter superseded memories before Execute sees them
	filtered := retrieve.FilterSuperseded(ret.Memories, reflectJSON)
	if removed := len(ret.Memories) - len(filtered); removed > 0 {
		fmt.Fprintf(os.Stderr, "filtered %d superseded memories\n", removed)
	}

	// Hand off to claude for execution
	result, err := executeViaClaude(sense.Raw, reflectJSON, filtered, ret.GitRefs)
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

	// Persist transcript + execute_meta (best-effort)
	{
		s, err := store.Open()
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
			// Persist file operations as execute_meta (merge with existing)
			if len(result.FilesTouched) > 0 || len(result.FilesRead) > 0 {
				mergeExecuteMeta(s.DB(), sense.SessionID, result.FilesTouched, result.FilesRead)
				fmt.Fprintf(os.Stderr, "files touched: %s\n", strings.Join(result.FilesTouched, ", "))
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
// maxTokens of 0 uses the default (512).
func senseViaAnthropic(apiKey, systemPrompt, userPrompt string, maxTokens int) (string, error) {
	if maxTokens <= 0 {
		maxTokens = 512
	}
	reqBody := map[string]any{
		"model":      "claude-haiku-4-5-20251001",
		"max_tokens": maxTokens,
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
// maxTokens of 0 uses the default (1024).
func senseViaOpenAI(apiKey, systemPrompt, userPrompt string, maxTokens int) (string, error) {
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	reqBody := map[string]any{
		"model":       "gpt-4.1-mini",
		"max_tokens":  maxTokens,
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

// executeResult holds the parsed output from a claude --print run.
type executeResult struct {
	ClaudeSessionID string   `json:"claude_session_id"`
	ResultText      string   `json:"result_text"`
	CostUSD         float64  `json:"cost_usd"`
	NumTurns        int      `json:"num_turns"`
	FullJSON        string   `json:"full_json"`      // raw output
	FilesTouched    []string `json:"files_touched"`   // files edited/written (from tool_use)
	FilesRead       []string `json:"files_read"`      // files read/grepped (from tool_use)
}

const executeSystemPrompt = `If the prompt is ambiguous about design intent — what something should look like, how it should behave, or what data it should show — stop and ask clarifying questions before implementing. Keep questions specific and few (max 3). A 30-second answer from the developer saves a wrong implementation.

Do not ask about things you can determine by reading the codebase. Only ask about decisions that require the developer's input.`

// executeViaClaude hands the user's prompt to claude --print --output-format stream-json
// and captures the session ID, transcript, result, and file operations.
// If filteredMemories is provided, they are included as the trusted memory
// list (superseded memories already removed by FilterSuperseded).
// gitRefs are relevant git commits, scored and budget-trimmed by the recall pipeline.
func executeViaClaude(userPrompt, reflectJSON string, filteredMemories []store.Scored, gitRefs []retrieve.GitRef) (*executeResult, error) {
	var memSection strings.Builder
	if len(filteredMemories) > 0 {
		memSection.WriteString("## Active memories (superseded entries removed)\n\n")
		now := time.Now().Unix()
		for _, m := range filteredMemories {
			age := int((now - m.UpdatedAt) / 86400)
			if age < 0 {
				age = 0
			}
			fmt.Fprintf(&memSection, "- %s (weight: %.2f, %dd ago)\n", m.TupleString(), m.Weight, age)
		}
		memSection.WriteString("\n")
	}

	var gitSection strings.Builder
	if len(gitRefs) > 0 {
		gitSection.WriteString("## Relevant git commits\n\n")
		for _, g := range gitRefs {
			fmt.Fprintf(&gitSection, "- %s %s (score: %.2f, %dd ago)\n", g.Hash, g.Message, g.Score, int(g.AgeDays))
		}
		gitSection.WriteString("\n")
	}

	combined := fmt.Sprintf("%s\n\n%s%s## Reflect analysis\n\n%s", userPrompt, memSection.String(), gitSection.String(), reflectJSON)
	return runClaudePrint([]string{"--print", "--verbose", "--output-format", "stream-json", "--system-prompt", executeSystemPrompt, "--allowedTools", "Read,Write,Edit,Grep,Glob,Bash"}, combined)
}

// resumeViaClaude resumes an existing Claude session with a new prompt.
func resumeViaClaude(claudeSessionID, userPrompt string) (*executeResult, error) {
	return runClaudePrint([]string{"--print", "--verbose", "--output-format", "stream-json", "--allowedTools", "Read,Write,Edit,Grep,Glob,Bash", "--resume", claudeSessionID}, userPrompt)
}

// streamProgress controls whether runClaudePrint streams live tool-use
// status to stderr as claude works. When false, output is buffered and
// only relayed after completion (original behaviour).
var streamProgress = true

// runClaude executes claude with the given args and stdin, parsing the JSON response.
func runClaudePrint(claudeArgs []string, stdin string) (*executeResult, error) {
	cwd, _ := os.Getwd()

	cmd := exec.Command("claude", claudeArgs...)
	cmd.Dir = cwd
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")
	cmd.Stdin = strings.NewReader(stdin)

	fmt.Fprintln(os.Stderr, "executing...")

	if streamProgress {
		return runClaudePrintStreaming(cmd)
	}
	return runClaudePrintBuffered(cmd)
}

// runClaudePrintBuffered captures all output and parses after completion.
func runClaudePrintBuffered(cmd *exec.Cmd) (*executeResult, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if se := stderr.String(); se != "" {
			fmt.Fprintf(os.Stderr, "claude stderr: %s\n", se)
		}
		if so := stdout.String(); so != "" {
			fmt.Fprintf(os.Stderr, "claude stdout: %s\n", so)
		}
		return nil, fmt.Errorf("claude: %w", err)
	}

	if se := stderr.String(); se != "" {
		fmt.Fprint(os.Stderr, se)
	}

	return parseClaudeJSON(stdout.String()), nil
}

// runClaudePrintStreaming reads NDJSON lines from claude's stdout as they
// arrive, extracts tool-use events, and emits short status lines to stderr.
func runClaudePrintStreaming(cmd *exec.Cmd) (*executeResult, error) {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude: %w", err)
	}

	var collected strings.Builder
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		collected.WriteString(line)
		collected.WriteByte('\n')

		// Try to extract a status line from this NDJSON event
		if status := extractStreamStatus(line); status != "" {
			fmt.Fprintf(os.Stderr, "▸ %s\n", status)
		}
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("claude: %w", err)
	}

	return parseClaudeJSON(collected.String()), nil
}

// extractStreamStatus parses a single NDJSON line from claude's stream-json
// output and returns a short human-readable status, or "" to skip.
func extractStreamStatus(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}

	var ev struct {
		Type    string `json:"type"`
		Message *struct {
			Role    string `json:"role"`
			Content []struct {
				Type  string `json:"type"`
				Text  string `json:"text"`
				Name  string `json:"name"`
				Input json.RawMessage `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal([]byte(line), &ev) != nil {
		return ""
	}

	if ev.Type != "assistant" || ev.Message == nil {
		return ""
	}

	for _, c := range ev.Message.Content {
		if c.Type == "tool_use" {
			return formatToolStatus(c.Name, c.Input)
		}
	}

	return ""
}

// formatToolStatus returns a short description of a tool invocation.
func formatToolStatus(name string, rawInput json.RawMessage) string {
	var input struct {
		FilePath string `json:"file_path"`
		Command  string `json:"command"`
		Pattern  string `json:"pattern"`
		Path     string `json:"path"`
		Content  string `json:"content"`
	}
	json.Unmarshal(rawInput, &input)

	switch name {
	case "Read":
		return fmt.Sprintf("Reading %s", shortPath(input.FilePath))
	case "Write":
		return fmt.Sprintf("Writing %s", shortPath(input.FilePath))
	case "Edit":
		return fmt.Sprintf("Editing %s", shortPath(input.FilePath))
	case "Grep":
		if input.Pattern != "" {
			return fmt.Sprintf("Searching for '%s'", truncate(input.Pattern, 40))
		}
		return "Searching"
	case "Glob":
		if input.Pattern != "" {
			return fmt.Sprintf("Finding files '%s'", truncate(input.Pattern, 40))
		}
		return "Finding files"
	case "Bash":
		if input.Command != "" {
			return fmt.Sprintf("Running: %s", truncate(input.Command, 60))
		}
		return "Running command"
	default:
		return name
	}
}

// shortPath returns the last two path segments for brevity.
func shortPath(p string) string {
	if p == "" {
		return "?"
	}
	parts := strings.Split(strings.ReplaceAll(p, "\\", "/"), "/")
	if len(parts) <= 2 {
		return p
	}
	return strings.Join(parts[len(parts)-2:], "/")
}

// truncate limits s to n characters, adding "…" if trimmed.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// parseClaudeJSON extracts session ID, result text, cost, turns, and file
// operations from claude output. Handles both JSON array and stream-json
// (NDJSON, one JSON object per line) formats.
func parseClaudeJSON(raw string) *executeResult {
	result := &executeResult{FullJSON: raw}

	// Collect all JSON messages — either from array or NDJSON lines
	var messages []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &messages); err != nil {
		// Try NDJSON (stream-json): one JSON object per line
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			messages = append(messages, json.RawMessage(line))
		}
	}

	touchedSet := make(map[string]bool)
	readSet := make(map[string]bool)

	for _, msg := range messages {
		var m struct {
			Type      string  `json:"type"`
			SessionID string  `json:"session_id"`
			Result    string  `json:"result"`
			CostUSD   float64 `json:"total_cost_usd"`
			NumTurns  int     `json:"num_turns"`
			Message   *struct {
				Content []struct {
					Type  string `json:"type"`
					Text  string `json:"text"`
					Name  string `json:"name"`
					Input struct {
						FilePath string `json:"file_path"`
						Command  string `json:"command"`
						Pattern  string `json:"pattern"`
						Path     string `json:"path"`
					} `json:"input"`
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
				if c.Type == "tool_use" {
					fp := c.Input.FilePath
					switch c.Name {
					case "Edit", "Write":
						if fp != "" {
							touchedSet[fp] = true
							readSet[fp] = true
						}
					case "Read":
						if fp != "" {
							readSet[fp] = true
						}
					case "Grep", "Glob":
						// Path for search tools
						p := c.Input.Path
						if p == "" {
							p = c.Input.FilePath
						}
						if p != "" {
							readSet[p] = true
						}
					}
				}
			}
		}
	}

	// Convert sets to sorted slices
	for f := range touchedSet {
		result.FilesTouched = append(result.FilesTouched, f)
	}
	sort.Strings(result.FilesTouched)
	for f := range readSet {
		result.FilesRead = append(result.FilesRead, f)
	}
	sort.Strings(result.FilesRead)

	return result
}

// runResume continues an open heb session by resuming the Claude conversation.
// Usage: heb resume [session_id] <prompt...>
func runResume(args []string) int {
	s, err := store.Open()
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
		// No Claude session — check if this is a clarification answer
		// (reflect done but no execute yet)
		senseJSON, sErr := store.ReadContract(s.DB(), sessionID, "sense")
		reflectJSON, rErr := store.ReadContract(s.DB(), sessionID, "reflect")
		if sErr == nil && rErr == nil {
			fmt.Fprintf(os.Stderr, "clarification received for session %s\n", sessionID)
			return resumeWithClarification(s, sessionID, senseJSON, reflectJSON, prompt)
		}
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

	// Persist file operations as execute_meta (merge with existing)
	if len(result.FilesTouched) > 0 || len(result.FilesRead) > 0 {
		mergeExecuteMeta(s.DB(), sessionID, result.FilesTouched, result.FilesRead)
		fmt.Fprintf(os.Stderr, "files touched: %s\n", strings.Join(result.FilesTouched, ", "))
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

// resumeWithClarification handles the case where reflect produced questions
// and the user is answering them before execute. It runs executeViaClaude
// with the original prompt + reflect context + clarification answers.
func resumeWithClarification(s *store.SQLiteStore, sessionID, senseJSON, reflectJSON, answers string) int {
	var sense struct {
		Raw string `json:"raw"`
	}
	if err := json.Unmarshal([]byte(senseJSON), &sense); err != nil {
		fmt.Fprintf(os.Stderr, "heb: parse sense: %v\n", err)
		return 1
	}

	// Store the clarification answers as a user turn
	store.WriteUserPrompt(s.DB(), sessionID, answers)

	// Build combined context: original prompt + clarification + memory context
	combined := fmt.Sprintf("%s\n\n## Clarification from developer\n\n%s\n\n## Memory context\n\n%s",
		sense.Raw, answers, reflectJSON)

	fmt.Fprintln(os.Stderr, "executing...")

	result, err := runClaudePrint([]string{"--print", "--verbose", "--output-format", "stream-json", "--system-prompt", executeSystemPrompt, "--allowedTools", "Read,Write,Edit,Grep,Glob,Bash"}, combined)
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

	// Persist transcript
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

	// Persist file operations as execute_meta (merge with existing)
	if len(result.FilesTouched) > 0 || len(result.FilesRead) > 0 {
		mergeExecuteMeta(s.DB(), sessionID, result.FilesTouched, result.FilesRead)
		fmt.Fprintf(os.Stderr, "files touched: %s\n", strings.Join(result.FilesTouched, ", "))
	}

	return 0
}

// mergeExecuteMeta reads the existing execute_meta contract (if any),
// unions the file sets with the new values, and writes back.
func mergeExecuteMeta(db *sql.DB, sessionID string, touched, read []string) {
	touchedSet := make(map[string]bool)
	readSet := make(map[string]bool)

	// Load existing
	existing, err := store.ReadContract(db, sessionID, "execute_meta")
	if err == nil {
		var meta struct {
			FilesTouched []string `json:"files_touched"`
			FilesRead    []string `json:"files_read"`
		}
		if json.Unmarshal([]byte(existing), &meta) == nil {
			for _, f := range meta.FilesTouched {
				touchedSet[f] = true
			}
			for _, f := range meta.FilesRead {
				readSet[f] = true
			}
		}
	}

	// Merge new
	for _, f := range touched {
		touchedSet[f] = true
	}
	for _, f := range read {
		readSet[f] = true
	}

	// Convert to sorted slices
	var allTouched, allRead []string
	for f := range touchedSet {
		allTouched = append(allTouched, f)
	}
	sort.Strings(allTouched)
	for f := range readSet {
		allRead = append(allRead, f)
	}
	sort.Strings(allRead)

	meta, _ := json.Marshal(map[string]any{
		"files_touched": allTouched,
		"files_read":    allRead,
	})
	store.WriteContract(db, sessionID, "execute_meta", string(meta))
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
