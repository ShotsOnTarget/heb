package retrieve

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/steelboltgames/heb/internal/store"
)

// Anchor is a single location where a symbol appears.
type Anchor struct {
	File string
	Line int
}

// SymbolAnchors pairs an extracted identifier with resolved locations.
// NotFound means the identifier was extracted from a memory but no file
// hit was found during the walk — a hint that the memory may be stale.
type SymbolAnchors struct {
	Symbol   string
	Hits     []Anchor
	NotFound bool
}

// Identifier extraction regexes. Precision is favoured over recall:
// matches should look unambiguously like code so the prompt stays clean.
var (
	// snake_case with 2+ underscore-separated parts (excludes trailing _leading form).
	reSnake = regexp.MustCompile(`\b[a-z][a-z0-9]*(?:_[a-z0-9]+){2,}\b`)
	// _leading_underscore method style (Godot/Python) — requires >=1 following underscore.
	reSnakeLead = regexp.MustCompile(`\b_[a-z][a-z0-9]*(?:_[a-z0-9]+)+\b`)
	// CamelCase with 2+ uppercase-led parts.
	reCamel = regexp.MustCompile(`\b[A-Z][a-z0-9]+(?:[A-Z][a-z0-9]+){1,}\b`)
	// Backticked short tokens.
	reBacktick = regexp.MustCompile("`([^`\\s]{3,64})`")
)

// stopSymbols filters natural-language snake-like phrases that would
// otherwise pass the regex but are never code.
var stopSymbols = map[string]bool{
	"obtained_via":                       true,
	"enemy_drops_and_home_base_crafting": true,
}

// ExtractIdentifiers scans memory bodies for code-like tokens.
// Returns a deduplicated, sorted slice, capped at maxSymbols most
// promising candidates (ranked by heuristic quality).
func ExtractIdentifiers(mems []store.Scored, maxSymbols int) []string {
	if maxSymbols <= 0 {
		maxSymbols = 30
	}
	type cand struct {
		sym   string
		score int
	}
	seen := make(map[string]int)

	add := func(sym string, weight int) {
		sym = strings.TrimSpace(sym)
		if len(sym) < 4 || stopSymbols[sym] {
			return
		}
		if prior, ok := seen[sym]; !ok || weight > prior {
			seen[sym] = weight
		}
	}
	for _, m := range mems {
		body := m.TupleString()
		for _, s := range reSnakeLead.FindAllString(body, -1) {
			add(s, 3) // leading underscore → very likely a method
		}
		for _, s := range reSnake.FindAllString(body, -1) {
			add(s, 2)
		}
		for _, s := range reCamel.FindAllString(body, -1) {
			add(s, 2)
		}
		for _, match := range reBacktick.FindAllStringSubmatch(body, -1) {
			add(match[1], 3)
		}
	}

	cands := make([]cand, 0, len(seen))
	for s, w := range seen {
		cands = append(cands, cand{s, w})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].sym < cands[j].sym
	})
	if len(cands) > maxSymbols {
		cands = cands[:maxSymbols]
	}
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.sym
	}
	sort.Strings(out)
	return out
}

// skipDirs are excluded from the walk — heavy build/vcs output.
var skipDirs = map[string]bool{
	".git":         true,
	".beads":       true,
	"node_modules": true,
	"dist":         true,
	"build":        true,
	"vendor":       true,
	"target":       true,
	".cache":       true,
	".venv":        true,
	"__pycache__":  true,
	".next":        true,
	".godot":       true,
}

// ResolveAnchors walks root and records file:line hits for each symbol.
// Each symbol is capped at maxHitsPerSymbol; files >maxFileBytes and
// probable binaries (null byte in first 4KB) are skipped.
func ResolveAnchors(root string, symbols []string, maxHitsPerSymbol int) []SymbolAnchors {
	out := make([]SymbolAnchors, 0, len(symbols))
	if len(symbols) == 0 || root == "" {
		for _, s := range symbols {
			out = append(out, SymbolAnchors{Symbol: s, NotFound: true})
		}
		return out
	}
	if maxHitsPerSymbol <= 0 {
		maxHitsPerSymbol = 3
	}

	escaped := make([]string, 0, len(symbols))
	for _, s := range symbols {
		escaped = append(escaped, regexp.QuoteMeta(s))
	}
	pat, err := regexp.Compile(`\b(?:` + strings.Join(escaped, "|") + `)\b`)
	if err != nil {
		for _, s := range symbols {
			out = append(out, SymbolAnchors{Symbol: s, NotFound: true})
		}
		return out
	}

	hits := make(map[string][]Anchor, len(symbols))
	const maxFileBytes = 1 << 20 // 1MB

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != root {
				if skipDirs[name] || (strings.HasPrefix(name, ".") && name != ".") {
					return filepath.SkipDir
				}
			}
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil || info.Size() > maxFileBytes {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		head := data
		if len(head) > 4096 {
			head = head[:4096]
		}
		if bytes.IndexByte(head, 0) >= 0 {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		line := 1
		start := 0
		for i := 0; i <= len(data); i++ {
			if i == len(data) || data[i] == '\n' {
				seg := data[start:i]
				for _, m := range pat.FindAll(seg, -1) {
					sym := string(m)
					if len(hits[sym]) < maxHitsPerSymbol {
						hits[sym] = append(hits[sym], Anchor{File: rel, Line: line})
					}
				}
				line++
				start = i + 1
			}
		}
		return nil
	})

	for _, s := range symbols {
		if h, ok := hits[s]; ok && len(h) > 0 {
			out = append(out, SymbolAnchors{Symbol: s, Hits: h})
		} else {
			out = append(out, SymbolAnchors{Symbol: s, NotFound: true})
		}
	}
	return out
}

// FormatAnchorSection renders a compact markdown section for the execute
// prompt. Symbols with hits come first; stale (not-found) entries follow,
// capped at maxNotFound so the prompt stays tight.
func FormatAnchorSection(anchors []SymbolAnchors, maxNotFound int) string {
	if len(anchors) == 0 {
		return ""
	}
	if maxNotFound < 0 {
		maxNotFound = 0
	}
	var found, missing []SymbolAnchors
	for _, a := range anchors {
		if a.NotFound {
			missing = append(missing, a)
		} else {
			found = append(found, a)
		}
	}
	if len(found) == 0 && len(missing) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Symbol anchors (from recalled memories)\n\n")
	for _, a := range found {
		locs := make([]string, len(a.Hits))
		for i, h := range a.Hits {
			locs[i] = h.File + ":" + itoa(h.Line)
		}
		b.WriteString("- `")
		b.WriteString(a.Symbol)
		b.WriteString("`: ")
		b.WriteString(strings.Join(locs, ", "))
		b.WriteString("\n")
	}
	if len(missing) > 0 && maxNotFound > 0 {
		shown := missing
		if len(shown) > maxNotFound {
			shown = shown[:maxNotFound]
		}
		b.WriteString("\n_Stale (no hits — memory may reference removed/renamed code):_ ")
		names := make([]string, len(shown))
		for i, m := range shown {
			names[i] = "`" + m.Symbol + "`"
		}
		b.WriteString(strings.Join(names, ", "))
		if len(missing) > maxNotFound {
			b.WriteString(", …")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}

// itoa avoids importing strconv just for int→string.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
