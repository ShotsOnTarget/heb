package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"

	"github.com/steelboltgames/heb/internal/store"
)

// ProjectInfo is a known project with session and memory counts.
type ProjectInfo struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	ActiveCount int    `json:"active_count"`
	TotalCount  int    `json:"total_count"`
	MemoryCount int    `json:"memory_count"`
}

func runProjects(_ []string) int {
	s, err := store.Open()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb projects: %v\n", err)
		return 1
	}
	defer s.Close()

	// Collect all known project paths: registered + those with data.
	known := make(map[string]*ProjectInfo)

	// 1. Registered projects (via heb init)
	registered, _ := store.ListProjects(s.DB())
	for _, r := range registered {
		known[r.Path] = &ProjectInfo{Path: r.Path, Name: r.Name}
	}

	// 2. Projects with sessions
	rows, err := s.DB().Query(`
		SELECT project,
		       COALESCE(SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END), 0),
		       COUNT(*)
		FROM sessions
		GROUP BY project
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var p string
			var active, total int
			if err := rows.Scan(&p, &active, &total); err != nil {
				continue
			}
			pi, ok := known[p]
			if !ok {
				pi = &ProjectInfo{Path: p, Name: path.Base(p)}
				known[p] = pi
			}
			pi.ActiveCount = active
			pi.TotalCount = total
		}
	}

	// 3. Memory counts from provenance
	for p, pi := range known {
		s.DB().QueryRow(`
			SELECT COUNT(DISTINCT memory_id) FROM provenance WHERE project = ?
		`, p).Scan(&pi.MemoryCount)
	}

	// Sort by most recent activity (total sessions desc, then name)
	var projects []ProjectInfo
	for _, pi := range known {
		projects = append(projects, *pi)
	}
	// Stable sort: projects with more sessions first, then alphabetical
	sortProjects(projects)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(projects)
	return 0
}

func sortProjects(ps []ProjectInfo) {
	for i := 1; i < len(ps); i++ {
		for j := i; j > 0; j-- {
			if ps[j].TotalCount > ps[j-1].TotalCount {
				ps[j], ps[j-1] = ps[j-1], ps[j]
			} else if ps[j].TotalCount == ps[j-1].TotalCount && ps[j].Name < ps[j-1].Name {
				ps[j], ps[j-1] = ps[j-1], ps[j]
			} else {
				break
			}
		}
	}
}
