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

// Default models used when the user hasn't picked a phase-specific model.
const (
	defaultAnthropicModel = "claude-haiku-4-5-20251001"
	defaultOpenAIModel    = "gpt-4.1-mini"
)

// resolveModel determines which provider, API key, and model to use for a
// given pipeline phase ("sense" or "reflect"). Honors `<phase>.model` config
// values in the form "api:<provider>:<model>" or "cli:<tool>[:<model>]" when
// set; otherwise falls back to resolveProvider() with the phase default.
//
// Provider values:
//
//	"anthropic"  — hit the Anthropic Messages API with apiKey/model.
//	"openai"     — hit the OpenAI Chat Completions API with apiKey/model.
//	"claude-cli" — spawn the claude CLI; model is the optional --model flag.
//	""           — no provider usable (caller should error).
func resolveModel(phase string) (provider, apiKey, model string) {
	fallback := func() (string, string, string) {
		p, k := resolveProvider()
		switch p {
		case "anthropic":
			return "anthropic", k, defaultAnthropicModel
		case "openai":
			return "openai", k, defaultOpenAIModel
		default:
			return "claude-cli", "", ""
		}
	}

	raw, _ := configLookup(phase+".model", false)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback()
	}

	parts := strings.SplitN(raw, ":", 3)
	if len(parts) < 2 {
		return fallback()
	}

	switch parts[0] {
	case "api":
		if len(parts) < 3 {
			return fallback()
		}
		switch parts[1] {
		case "anthropic":
			if k, _ := configLookup("anthropic-key", false); k != "" {
				return "anthropic", k, parts[2]
			}
		case "openai":
			if k, _ := configLookup("openai-key", false); k != "" {
				return "openai", k, parts[2]
			}
		}
	case "cli":
		cliModel := ""
		if len(parts) >= 3 {
			cliModel = parts[2]
		}
		switch parts[1] {
		case "claude":
			return "claude-cli", "", cliModel
		case "gemini":
			// Gemini CLI routing not implemented — fall back.
			fmt.Fprintf(os.Stderr, "heb: %s.model=cli:gemini is not wired up yet; falling back\n", phase)
		}
	}

	return fallback()
}

// apiUsage captures token counts reported by an LLM API call.
type apiUsage struct {
	In  int
	Out int
}

// modelLabel produces a short human-readable name for a model. If model is
// empty, the fallback is used verbatim; otherwise trailing dated suffixes
// like "-20251001" are stripped so the GUI can show a clean name.
func modelLabel(model, fallback string) string {
	if model == "" {
		model = fallback
	}
	// Strip a trailing date suffix (e.g. "claude-haiku-4-5-20251001" → "claude-haiku-4-5").
	if i := strings.LastIndex(model, "-"); i > 0 {
		tail := model[i+1:]
		if len(tail) == 8 {
			allDigits := true
			for _, r := range tail {
				if r < '0' || r > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				model = model[:i]
			}
		}
	}
	return model
}

// emitStats writes a stats line to stderr for the GUI / log to pick up.
// Format: "stats: <phase> in=<N> out=<N> ms=<N> [extra k=v ...]"
func emitStats(phase string, u apiUsage, dur time.Duration, extras ...string) {
	suffix := ""
	if len(extras) > 0 {
		suffix = " " + strings.Join(extras, " ")
	}
	fmt.Fprintf(os.Stderr, "stats: %s in=%d out=%d ms=%d%s\n",
		phase, u.In, u.Out, dur.Milliseconds(), suffix)
}

// emitPrepareProgress streams the anchor-resolution result to stderr so the
// GUI can render it as a "Prepare" chat entry between Predict and Claude.
// Line shape mirrors recall: a header line plus one detail line per symbol.
func emitPrepareProgress(anchors []retrieve.SymbolAnchors, dur time.Duration) {
	hits, stale := 0, 0
	for _, a := range anchors {
		if a.NotFound {
			stale++
			continue
		}
		hits += len(a.Hits)
	}
	fmt.Fprintf(os.Stderr, "prepare: %d symbols, %d hits, %d stale\n", len(anchors), hits, stale)
	for _, a := range anchors {
		if a.NotFound {
			fmt.Fprintf(os.Stderr, "prepare-stale: `%s`\n", a.Symbol)
			continue
		}
		locs := make([]string, len(a.Hits))
		for i, h := range a.Hits {
			locs[i] = fmt.Sprintf("%s:%d", h.File, h.Line)
		}
		fmt.Fprintf(os.Stderr, "prepare-hit: `%s`: %s\n", a.Symbol, strings.Join(locs, ", "))
	}
	fmt.Fprintf(os.Stderr, "stats: prepare in=0 out=0 ms=%d symbols=%d hits=%d stale=%d\n",
		dur.Milliseconds(), len(anchors), hits, stale)
}

// senseViaAnthropic calls the Anthropic Messages API.
// model defaults to defaultAnthropicModel when empty.
// maxTokens of 0 uses the default (512).
func senseViaAnthropic(apiKey, model, systemPrompt, userPrompt string, maxTokens int) (string, apiUsage, error) {
	if maxTokens <= 0 {
		maxTokens = 512
	}
	if model == "" {
		model = defaultAnthropicModel
	}
	reqBody := map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"system":     systemPrompt,
		"messages": []map[string]string{
			{"role": "user", "content": userPrompt},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", apiUsage{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", apiUsage{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Anthropic-Version", "2023-06-01")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", apiUsage{}, fmt.Errorf("api call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", apiUsage{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", apiUsage{}, fmt.Errorf("api returned %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", apiUsage{}, fmt.Errorf("parse response: %w", err)
	}
	if len(apiResp.Content) == 0 {
		return "", apiUsage{}, fmt.Errorf("empty response from api")
	}

	return apiResp.Content[0].Text, apiUsage{In: apiResp.Usage.InputTokens, Out: apiResp.Usage.OutputTokens}, nil
}

// senseViaOpenAI calls the OpenAI Chat Completions API.
// model defaults to defaultOpenAIModel when empty.
// maxTokens of 0 uses the default (1024).
func senseViaOpenAI(apiKey, model, systemPrompt, userPrompt string, maxTokens int) (string, apiUsage, error) {
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	if model == "" {
		model = defaultOpenAIModel
	}
	// Reasoning-era models (o-series, gpt-5.x) require max_completion_tokens
	// and reject temperature overrides.
	isReasoning := strings.HasPrefix(model, "o") || strings.HasPrefix(model, "gpt-5")
	systemRole := "system"
	if isReasoning {
		systemRole = "developer"
	}
	reqBody := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": systemRole, "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
	}
	if isReasoning {
		reqBody["max_completion_tokens"] = maxTokens
	} else {
		reqBody["max_tokens"] = maxTokens
		reqBody["temperature"] = 0
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", apiUsage{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", apiUsage{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", apiUsage{}, fmt.Errorf("api call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", apiUsage{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", apiUsage{}, fmt.Errorf("api returned %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", apiUsage{}, fmt.Errorf("parse response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return "", apiUsage{}, fmt.Errorf("empty response from api")
	}

	return apiResp.Choices[0].Message.Content, apiUsage{In: apiResp.Usage.PromptTokens, Out: apiResp.Usage.CompletionTokens}, nil
}

// executeResult holds the parsed output from a claude --print run.
type executeResult struct {
	ClaudeSessionID string   `json:"claude_session_id"`
	ResultText      string   `json:"result_text"`
	CostUSD         float64  `json:"cost_usd"`
	NumTurns        int      `json:"num_turns"`
	InputTokens     int      `json:"input_tokens"`    // from final result.usage.input_tokens
	OutputTokens    int      `json:"output_tokens"`   // from final result.usage.output_tokens
	FullJSON        string   `json:"full_json"`       // raw output
	FilesTouched    []string `json:"files_touched"`   // files edited/written (from tool_use)
	FilesRead       []string `json:"files_read"`      // files read/grepped (from tool_use)
}

const executeSystemPrompt = `If the prompt is ambiguous about design intent — what something should look like, how it should behave, or what data it should show — stop and ask clarifying questions before implementing. Keep questions specific and few (max 3). A 30-second answer from the developer saves a wrong implementation.

Do not ask about things you can determine by reading the codebase. Only ask about decisions that require the developer's input.

## Session Context

You receive four sections at conversation start:
- **Active memories** — observations about the codebase.
- **Symbol anchors** — identifiers from those memories pre-resolved to ` + "`file:line`" + `. Jump straight to these locations instead of re-grepping the symbols. Anchors listed under *Stale* had no hits in the current tree — treat the referencing memory as possibly outdated.
- **Relevant git commits** — recent commits scored against the prompt. Use them for recency context (e.g. "X was refactored 2 days ago").
- **Reflect analysis** — a prediction with risk assessment.

Before executing any change:
- Read the memories. They tell you how things are, not how they must be. If a requested change diverges from an observed pattern, that's worth mentioning — not blocking.
- Read the prediction risks. If a risk has medium or higher confidence, surface it to the user with your assessment of whether it applies.
- If memories suggest the request is part of a broader pattern, ask about scope before applying narrowly.

You are not enforcing rules. You are providing informed judgment.

Weight indicates confidence: >=0.70 high, 0.40-0.69 moderate, <0.40 speculative. Source [match] means directly relevant; [edge] means associated context from memory graph.`

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

		// Group memories by topic for easier scanning.
		type memEntry struct {
			body   string
			weight float64
			age    int
			source string
		}
		groups := make(map[string][]memEntry)
		var groupOrder []string
		for _, m := range filteredMemories {
			age := int((now - m.UpdatedAt) / 86400)
			if age < 0 {
				age = 0
			}
			topic := humanizeTopic(m.TopicTokens)
			if _, exists := groups[topic]; !exists {
				groupOrder = append(groupOrder, topic)
			}
			groups[topic] = append(groups[topic], memEntry{
				body:   humanizeBody(m.TupleString()),
				weight: m.Weight,
				age:    age,
				source: m.Source,
			})
		}

		for _, topic := range groupOrder {
			entries := groups[topic]
			fmt.Fprintf(&memSection, "### %s\n", topic)
			for _, e := range entries {
				fmt.Fprintf(&memSection, "- [%s] %s (weight: %.2f, %dd ago)\n",
					e.source, e.body, e.weight, e.age)
			}
			memSection.WriteString("\n")
		}
	}

	var gitSection strings.Builder
	if len(gitRefs) > 0 {
		gitSection.WriteString("## Relevant git commits\n\n")
		for _, g := range gitRefs {
			fmt.Fprintf(&gitSection, "- %s %s (score: %.2f, %dd ago)\n", g.Hash, g.Message, g.Score, int(g.AgeDays))
		}
		gitSection.WriteString("\n")
	}

	// Pre-resolve high-confidence identifiers from memories to file:line anchors.
	// Saves early agent turns spent re-grepping symbols the memories already named.
	// Stale symbols (no hits) are flagged so the agent treats the memory as possibly outdated.
	var anchorSection string
	if cwd, wdErr := os.Getwd(); wdErr == nil && len(filteredMemories) > 0 {
		symbols := retrieve.ExtractIdentifiers(filteredMemories, 30)
		if len(symbols) > 0 {
			prepStart := time.Now()
			anchors := retrieve.ResolveAnchors(cwd, symbols, 3)
			emitPrepareProgress(anchors, time.Since(prepStart))
			anchorSection = retrieve.FormatAnchorSection(anchors, 5)
		}
	}

	combined := fmt.Sprintf("%s\n\n%s%s%s## Reflect analysis\n\n%s", userPrompt, memSection.String(), anchorSection, gitSection.String(), reflectJSON)
	start := time.Now()
	result, err := runClaudePrint([]string{"--print", "--verbose", "--output-format", "stream-json", "--permission-mode", "acceptEdits", "--system-prompt", executeSystemPrompt, "--allowedTools", "Read,Write,Edit,Grep,Glob,Bash"}, combined)
	if err == nil && result != nil {
		emitStats("execute", apiUsage{In: result.InputTokens, Out: result.OutputTokens}, time.Since(start),
			fmt.Sprintf("turns=%d", result.NumTurns),
			fmt.Sprintf("cost=$%.4f", result.CostUSD))
	}
	return result, err
}

// resumeViaClaude resumes an existing Claude session with a new prompt.
func resumeViaClaude(claudeSessionID, userPrompt string) (*executeResult, error) {
	start := time.Now()
	result, err := runClaudePrint([]string{"--print", "--verbose", "--output-format", "stream-json", "--permission-mode", "acceptEdits", "--allowedTools", "Read,Write,Edit,Grep,Glob,Bash", "--resume", claudeSessionID}, userPrompt)
	if err == nil && result != nil {
		emitStats("execute", apiUsage{In: result.InputTokens, Out: result.OutputTokens}, time.Since(start),
			fmt.Sprintf("turns=%d", result.NumTurns),
			fmt.Sprintf("cost=$%.4f", result.CostUSD))
	}
	return result, err
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

	fmt.Fprintf(os.Stderr, "executing: %s\n", formatClaudeArgsForDisplay(claudeArgs))

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

// humanizeBody converts underscore-encoded memory bodies to readable text.
// "CLI_layer_and_pipeline_orchestration" → "CLI layer and pipeline orchestration"
// Already-natural bodies pass through unchanged.
func humanizeBody(body string) string {
	// Don't touch bodies that already have spaces (natural language)
	if strings.Contains(body, " ") {
		return body
	}
	return strings.ReplaceAll(body, "_", " ")
}

// humanizeTopic converts comma-separated topic tokens to a section header.
// "review,codebase,architecture" → "Review / Codebase / Architecture"
// Empty topic tokens become "General".
func humanizeTopic(topicTokens string) string {
	topicTokens = strings.TrimSpace(topicTokens)
	if topicTokens == "" {
		return "General"
	}
	parts := strings.Split(topicTokens, ",")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " / ")
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
			Usage     struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
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
			result.InputTokens = m.Usage.InputTokens
			result.OutputTokens = m.Usage.OutputTokens
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
func senseViaClaude(cwd, model, systemPrompt, userPrompt string) (string, apiUsage, error) {
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
	cmd.Stderr = os.Stderr

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", apiUsage{}, fmt.Errorf("claude --print: %w", err)
	}

	return stdout.String(), apiUsage{}, nil
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

	start := time.Now()
	result, err := runClaudePrint([]string{"--print", "--verbose", "--output-format", "stream-json", "--permission-mode", "acceptEdits", "--system-prompt", executeSystemPrompt, "--allowedTools", "Read,Write,Edit,Grep,Glob,Bash"}, combined)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb: %v\n", err)
		return 1
	}
	emitStats("execute", apiUsage{In: result.InputTokens, Out: result.OutputTokens}, time.Since(start),
		fmt.Sprintf("turns=%d", result.NumTurns),
		fmt.Sprintf("cost=$%.4f", result.CostUSD))

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

// formatClaudeArgsForDisplay renders claude argv for GUI/log visibility,
// eliding long flag values (e.g. --system-prompt) so the line stays readable.
func formatClaudeArgsForDisplay(args []string) string {
	out := []string{"claude"}
	for i := 0; i < len(args); i++ {
		a := args[i]
		out = append(out, a)
		if strings.HasPrefix(a, "--") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			v := args[i+1]
			if len(v) > 80 {
				v = fmt.Sprintf("<%d bytes>", len(v))
			} else if strings.ContainsAny(v, " \t\"") {
				v = fmt.Sprintf("%q", v)
			} else if v == "" {
				v = `""`
			}
			out = append(out, v)
			i++
		}
	}
	return strings.Join(out, " ")
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
