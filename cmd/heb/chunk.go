package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/steelboltgames/heb/internal/consolidate"
	"github.com/steelboltgames/heb/internal/memory"
)

const chunkSystemPrompt = `You are a memory compressor. Given a verbose observation, split it into 1-4 terse atoms.

Rules:
- Each atom ≤ 12 words
- No filler words (implements, with, functions for, as a, etc.)
- Noun-verb-noun preferred: "CombatScreen syncs combat state"
- Preserve file paths, class names, and technical terms exactly
- One atom per line, no numbering, no bullets
- If the input is already terse (≤ 12 words), return it unchanged`

// chunkLessons splits any verbose lessons into terse atoms via a cheap LLM call.
// Lessons at or below AtomTokenCap are left untouched. Returns the modified
// lesson slice. On API failure, returns lessons unchanged (graceful degradation).
func chunkLessons(lessons []consolidate.Lesson) []consolidate.Lesson {
	_, apiKey := resolveProvider()
	if apiKey == "" {
		return lessons
	}

	var result []consolidate.Lesson
	for _, l := range lessons {
		tc := memory.TokenCount(l.Body)
		if tc <= memory.AtomTokenCap {
			result = append(result, l)
			continue
		}

		chunks, err := chunkViaAPI(apiKey, l.Body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "heb: chunk failed, keeping original: %v\n", err)
			result = append(result, l)
			continue
		}

		fmt.Fprintf(os.Stderr, "  chunked %d→%d atoms: %q\n", tc, len(chunks), l.Body[:min(60, len(l.Body))])
		for _, chunk := range chunks {
			result = append(result, consolidate.Lesson{
				Body:       chunk,
				Scope:      l.Scope,
				Confidence: l.Confidence,
				Evidence:   l.Evidence,
			})
		}
	}
	return result
}

// chunkViaAPI calls the cheap model to split a verbose body into terse atoms.
func chunkViaAPI(apiKey, body string) ([]string, error) {
	provider, _ := resolveProvider()

	var raw string
	var err error
	switch provider {
	case "anthropic":
		raw, err = senseViaAnthropic(apiKey, chunkSystemPrompt, body)
	case "openai":
		raw, err = senseViaOpenAI(apiKey, chunkSystemPrompt, body)
	default:
		return nil, fmt.Errorf("no api provider configured")
	}
	if err != nil {
		return nil, err
	}

	// Parse: one atom per line
	var atoms []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		// Strip bullet prefixes the LLM might add despite instructions
		line = strings.TrimLeft(line, "-•*0123456789. ")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		atoms = append(atoms, line)
	}

	if len(atoms) == 0 {
		return nil, fmt.Errorf("chunker returned no atoms")
	}

	return atoms, nil
}
