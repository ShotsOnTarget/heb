package retrieve

import "github.com/steelboltgames/heb/internal/store"

// Input is the parsed contract:sense>recall plus any recall-side overrides the
// CLI is allowed to carry. Only Tokens drives retrieval — SessionID
// and Project are echoed into the output.
type Input struct {
	SessionID string
	Project   string
	Tokens    []string
}

// Run is the package entry point: takes a contract:sense>recall input, executes
// all passes, trims to budget, and returns a Result. Memories come
// from the caller (the CLI resolves store.Recall and passes them in)
// so this function is pure w.r.t. the filesystem — only the runner
// touches the world.
func Run(in Input, memories []store.Scored, runner Runner, cfg Config) *Result {
	if memories == nil {
		memories = []store.Scored{}
	}
	gitRefs := gitPass(in.Tokens, runner, cfg)
	beadRefs := beadsPass(in.Tokens, runner, cfg)

	r := &Result{
		SessionID:   in.SessionID,
		Project:     in.Project,
		TokenBudget: cfg.TokenBudget,
		Memories:    memories,
		GitRefs:     gitRefs,
		Beads:       beadRefs,
	}
	trimToBudget(r, cfg, measureRender)
	return r
}
