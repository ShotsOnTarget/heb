package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/steelboltgames/heb/internal/store"
)

var validConfigs = map[string][]string{
	"verbosity":     {"loud", "quiet", "mute"},
	"provider":      {"anthropic", "openai"},
	"anthropic-key": nil, // freeform
	"openai-key":    nil, // freeform
	"sense.model":   nil, // freeform — e.g. "", "api:anthropic:claude-haiku-4-5", "cli:claude:sonnet-4-6"
	"reflect.model": nil, // freeform — same scheme as sense.model
	"learn.model":   nil, // freeform — e.g. "resume", "gpt-5.4", "gpt-4.1-mini"
}

var configDefaults = map[string]string{
	"verbosity":     "quiet",
	"provider":      "anthropic",
	"anthropic-key": "",
	"openai-key":    "",
	"sense.model":   "",
	"reflect.model": "",
	"learn.model":   "resume",
}

// globalConfigKeys are keys that make sense at the global (~/.heb) level.
var globalConfigKeys = map[string]bool{
	"provider":      true,
	"anthropic-key": true,
	"openai-key":    true,
	"sense.model":   true,
	"reflect.model": true,
	"learn.model":   true,
}

// isSensitiveKey returns true for keys whose values should be masked on display.
func isSensitiveKey(key string) bool {
	return key == "anthropic-key" || key == "openai-key"
}

func runConfig(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: heb config <get|set> [--global] <key> [value]")
		fmt.Fprintln(os.Stderr, "keys:")
		for k, vals := range validConfigs {
			g := ""
			if globalConfigKeys[k] {
				g = " (global)"
			}
			if vals == nil {
				fmt.Fprintf(os.Stderr, "  %-12s  <string>%s\n", k, g)
			} else {
				fmt.Fprintf(os.Stderr, "  %-12s  %s (default: %s)%s\n", k, strings.Join(vals, " | "), configDefaults[k], g)
			}
		}
		return 2
	}
	switch args[0] {
	case "get":
		return configGet(args[1:])
	case "set":
		return configSet(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "heb config: unknown subcommand %q\n", args[0])
		return 2
	}
}

// parseGlobalFlag extracts --global from args, returning the flag and remaining args.
func parseGlobalFlag(args []string) (bool, []string) {
	var global bool
	var rest []string
	for _, a := range args {
		if a == "--global" {
			global = true
		} else {
			rest = append(rest, a)
		}
	}
	return global, rest
}

func configGet(args []string) int {
	global, args := parseGlobalFlag(args)
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: heb config get [--global] <key>")
		return 2
	}
	key := args[0]
	if _, ok := validConfigs[key]; !ok {
		fmt.Fprintf(os.Stderr, "heb config get: unknown key %q\n", key)
		return 1
	}

	val, source := configLookup(key, global)
	// Always print full value to stdout (for piping).
	// Mask on stderr only.
	fmt.Println(val)
	if isSensitiveKey(key) && len(val) > 10 {
		fmt.Fprintf(os.Stderr, "%s...%s (%s)\n", val[:7], val[len(val)-4:], source)
	} else if source != "" {
		fmt.Fprintf(os.Stderr, "%s (%s)\n", val, source)
	}
	return 0
}

// configLookup resolves a config value with cascade: store → env → default.
// The globalOnly parameter is kept for API compatibility but has no effect
// since all config now lives in the single global store.
func configLookup(key string, globalOnly bool) (string, string) {
	// Try global store
	gs, err := store.OpenOrInit()
	if err == nil {
		defer gs.Close()
		if val, err := store.ConfigGet(gs.DB(), key); err == nil {
			return val, "global"
		}
	}

	// Try env
	switch key {
	case "anthropic-key":
		if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
			return v, "env"
		}
	case "openai-key":
		if v := os.Getenv("OPENAI_API_KEY"); v != "" {
			return v, "env"
		}
	}

	return configDefaults[key], "default"
}

func configSet(args []string) int {
	global, args := parseGlobalFlag(args)
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: heb config set [--global] <key> <value>")
		return 2
	}
	key, value := args[0], args[1]
	allowed, ok := validConfigs[key]
	if !ok {
		fmt.Fprintf(os.Stderr, "heb config set: unknown key %q\n", key)
		return 1
	}
	// nil means freeform — any non-empty value accepted
	if allowed != nil {
		valid := false
		for _, v := range allowed {
			if value == v {
				valid = true
				break
			}
		}
		if !valid {
			fmt.Fprintf(os.Stderr, "heb config set: %q must be one of: %s\n", key, strings.Join(allowed, ", "))
			return 1
		}
	}

	// Auto-promote sensitive keys to global if not explicitly local
	if !global && globalConfigKeys[key] {
		global = true
	}

	label := "global"
	s, err := store.OpenOrInit()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb config set: %v\n", err)
		return 1
	}
	defer s.Close()

	if err := store.ConfigSet(s.DB(), key, value); err != nil {
		fmt.Fprintf(os.Stderr, "heb config set: %v\n", err)
		return 1
	}
	if isSensitiveKey(key) && len(value) > 10 {
		fmt.Fprintf(os.Stderr, "%s=%s...%s (%s)\n", key, value[:7], value[len(value)-4:], label)
	} else {
		fmt.Fprintf(os.Stderr, "%s=%s (%s)\n", key, value, label)
	}
	return 0
}
