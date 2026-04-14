package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/steelboltgames/heb/internal/consolidate"
	"github.com/steelboltgames/heb/internal/store"
)

// runConsolidate is the `heb consolidate` entry point. It defaults to
// contract:learn>consolidate mode: reads a /learn output on stdin, runs the
// consolidate.Run translator to produce a Payload, then applies the
// Payload to the store inside a single transaction.
//
// The legacy explicit-payload mode (pre-yx0) is preserved behind
// --raw — it skips the translator and reads a pre-computed payload
// matching consolidate.Payload.
func runConsolidate(args []string) int {
	fs := flag.NewFlagSet("consolidate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	cfg := consolidate.DefaultConfig()
	fs.Float64Var(&cfg.NewGain, "new-gain", cfg.NewGain, "delta for newly created memories")
	fs.Float64Var(&cfg.ReinforceGain, "reinforce-gain", cfg.ReinforceGain, "delta for existing memories")
	fs.Float64Var(&cfg.CoActivationBoost, "co-activation-boost", cfg.CoActivationBoost, "edge delta when tuples written together")
	fs.Float64Var(&cfg.EntanglementScale, "entanglement-scale", cfg.EntanglementScale, "scale applied to peak_intensity for entanglement signals")
	fs.Float64Var(&cfg.EntanglementMin, "entanglement-min", cfg.EntanglementMin, "most negative entanglement signal (lower clamp)")
	fs.Float64Var(&cfg.EntanglementMax, "entanglement-max", cfg.EntanglementMax, "least negative entanglement signal (upper clamp)")
	fs.Float64Var(&cfg.EdgeDecayRate, "edge-decay-rate", cfg.EdgeDecayRate, "negative delta per session for unconfirmed edge recalls")
	fs.IntVar(&cfg.EdgeEstablishThreshold, "edge-establish-threshold", cfg.EdgeEstablishThreshold, "co_activation_count >= this means edge is established")
	fs.Float64Var(&cfg.MinConfidence, "min-confidence", cfg.MinConfidence, "drop lessons below this confidence")
	fs.StringVar(&cfg.Format, "format", cfg.Format, "output format: both | human | json")
	raw := fs.Bool("raw", false, "read explicit Payload JSON on stdin; skip the LearnResult translator")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "heb consolidate: %v\n", err)
		return 2
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb consolidate: read stdin: %v\n", err)
		return 1
	}

	var result consolidate.Result
	if *raw {
		result, err = runRawPayload(data)
	} else {
		result, err = runLearnResult(data, cfg)
	}
	if err != nil {
		// Always emit a Result-shaped JSON on stdout so callers can parse it uniformly.
		result.Errors = append(result.Errors, err.Error())
		emit(result, cfg.Format)
		fmt.Fprintf(os.Stderr, "heb consolidate: %v\n", err)
		return 1
	}

	emit(result, cfg.Format)
	fmt.Fprintln(os.Stderr, consolidate.StderrSummary(result))
	return 0
}

// runLearnResult parses a contract:learn>consolidate JSON blob, runs the translator to
// produce a Payload, and applies the Payload to the store.
func runLearnResult(data []byte, cfg consolidate.Config) (consolidate.Result, error) {
	var c consolidate.LearnResult
	if err := json.Unmarshal(data, &c); err != nil {
		return consolidate.Result{}, fmt.Errorf("parse contract:learn>consolidate: %w", err)
	}
	c.Raw = append(json.RawMessage(nil), data...)

	if c.Project == "" {
		return consolidate.Result{SessionID: c.SessionID}, fmt.Errorf("project required")
	}

	result := consolidate.Run(c, cfg)
	if err := applyPayload(&result, &c, &cfg); err != nil {
		return result, err
	}
	return result, nil
}

// runRawPayload is the legacy path: read an explicit consolidate.Payload
// JSON (the shape cmd/heb used pre-yx0) and apply it directly.
func runRawPayload(data []byte) (consolidate.Result, error) {
	var p consolidate.Payload
	if err := json.Unmarshal(data, &p); err != nil {
		return consolidate.Result{}, fmt.Errorf("parse raw payload: %w", err)
	}
	if p.Project == "" {
		return consolidate.Result{SessionID: p.SessionID}, fmt.Errorf("project required")
	}
	result := consolidate.Result{
		SessionID:    p.SessionID,
		Project:      p.Project,
		ThresholdMet: true,
		Payload:      p,
		Applied:      []consolidate.MemoryApply{},
		Skipped:      p.Skipped,
		Errors:       []string{},
	}
	if err := applyPayload(&result, nil, nil); err != nil {
		return result, err
	}
	return result, nil
}

// applyPayload runs the full store transaction against result.Payload,
// filling in the post-apply fields on the result. When lr and cfg are
// non-nil, also runs the edge decay pass for unconfirmed edge recalls.
func applyPayload(result *consolidate.Result, lr *consolidate.LearnResult, cfg *consolidate.Config) error {
	p := result.Payload

	root, err := store.RepoRoot()
	if err != nil {
		return err
	}
	s, err := store.Open(root)
	if err != nil {
		return err
	}
	defer s.Close()

	// Capture git HEAD for traceability — best-effort, non-fatal.
	if out, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		p.CommitHash = strings.TrimSpace(string(out))
		result.Payload = p
	}

	tx, err := s.DB().Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if err := applyMemoryDeltas(tx, p, result); err != nil {
		tx.Rollback()
		return err
	}
	if err := applyEdgeDeltas(tx, p, result); err != nil {
		tx.Rollback()
		return err
	}
	if lr!= nil && cfg != nil {
		if err := applyEdgeDecay(tx, s.DB(), lr, cfg, result); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := applyEpisode(tx, p, result); err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func applyMemoryDeltas(tx *sql.Tx, p consolidate.Payload, result *consolidate.Result) error {
	for _, md := range p.Memories {
		id, w, wasNew, err := store.ApplyMemoryEvent(
			tx,
			md.Body,
			md.Event, md.Reason,
			p.SessionID, p.BeadID, p.TopicTokens, p.CommitHash,
			md.DeltaNew, md.DeltaReinforce,
		)
		if err != nil {
			return fmt.Errorf("apply memory: %w", err)
		}
		if err := store.AddProvenance(tx, id, p.Project, p.SessionID, p.BeadID); err != nil {
			return fmt.Errorf("provenance: %w", err)
		}
		eventKind := md.Event
		if wasNew {
			eventKind = "created"
		}
		result.Applied = append(result.Applied, consolidate.MemoryApply{
			ID: id, Body: md.Body,
			Event: eventKind, NewWeight: w, WasNew: wasNew,
		})
		if md.Event == "entanglement_signal" {
			result.EntanglementSignals++
		}
	}
	return nil
}

func applyEdgeDeltas(tx *sql.Tx, p consolidate.Payload, result *consolidate.Result) error {
	for _, ed := range p.Edges {
		aID := store.MemoryID(ed.ABody)
		bID := store.MemoryID(ed.BBody)
		if err := store.UpdateEdge(tx, aID, bID, ed.Delta, ed.CoActivation); err != nil {
			return fmt.Errorf("edge: %w", err)
		}
		result.EdgesUpdated++
	}
	return nil
}

func applyEpisode(tx *sql.Tx, p consolidate.Payload, result *consolidate.Result) error {
	if p.Episode == nil {
		return nil
	}
	written, err := store.WriteEpisode(tx, p.Episode.SessionID, string(p.Episode.Payload))
	if err != nil {
		return fmt.Errorf("episode: %w", err)
	}
	result.EpisodeWritten = written
	return nil
}

// applyEdgeDecay weakens edges via two paths:
//
//  1. Unconfirmed recall: edges that fired via spreading activation but
//     whose target memories were not confirmed by a lesson. Only runs
//     for act sessions (files_touched > 0 or correction_count > 0).
//
//  2. Prediction contradiction: edges whose source tuples contributed to
//     a prediction that was wrong. Runs regardless of session type —
//     a wrong prediction is direct evidence the edge is misleading.
//     Uses 2× EdgeDecayRate for the stronger signal.
//
// Both paths only decay young edges (co_activation_count < EdgeEstablishThreshold).
func applyEdgeDecay(tx *sql.Tx, db *sql.DB, lr *consolidate.LearnResult, cfg *consolidate.Config, result *consolidate.Result) error {
	if cfg.EdgeDecayRate <= 0 {
		return nil
	}

	// Track which edges we've already decayed to avoid double-penalising.
	decayed := make(map[[2]string]bool)

	// Path 1: Unconfirmed edge recalls (act sessions only).
	if len(lr.RecalledViaEdges) > 0 && result.ThresholdMet &&
		(len(lr.Implementation.FilesTouched) > 0 || lr.CorrectionCount > 0) {

		writtenSet := make(map[string]bool, len(lr.Lessons))
		for _, l := range lr.Lessons {
			writtenSet[l.Body] = true
		}

		for _, tuple := range lr.RecalledViaEdges {
			if writtenSet[tuple] {
				continue
			}
			decayEdgesForTuple(tx, db, tuple, -cfg.EdgeDecayRate, cfg.EdgeEstablishThreshold, decayed, result)
		}
	}

	// Path 2: Prediction contradictions (any session type).
	if lr.PredictionReconciliation != nil && !lr.PredictionReconciliation.ColdStart {
		// Collect edge-sourced recall tuples for intersection.
		edgeRecalled := make(map[string]bool, len(lr.RecalledViaEdges))
		for _, t := range lr.RecalledViaEdges {
			edgeRecalled[t] = true
		}

		for _, elem := range lr.PredictionReconciliation.Elements {
			if elem.Event != "prediction_contradicted" {
				continue
			}
			for _, tuple := range elem.SourceTuples {
				// Only decay if this tuple was delivered via an edge.
				// Direct-match memories are not edge failures.
				if !edgeRecalled[tuple] {
					continue
				}
				decayEdgesForTuple(tx, db, tuple, -cfg.EdgeDecayRate*2, cfg.EdgeEstablishThreshold, decayed, result)
			}
		}
	}

	return nil
}

// decayEdgesForTuple decays all young edges involving the given tuple.
// Skips edges already in the decayed set to avoid double-penalising.
func decayEdgesForTuple(tx *sql.Tx, db *sql.DB, tuple string, delta float64, threshold int, decayed map[[2]string]bool, result *consolidate.Result) {
	id := tupleToMemoryID(tuple)
	if id == "" {
		return
	}
	edges, err := store.EdgesFor(db, id)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("edge decay query %s: %v", tuple, err))
		return
	}
	for _, e := range edges {
		if e.CoActivationCount >= threshold {
			continue
		}
		key := [2]string{e.AID, e.BID}
		if e.AID > e.BID {
			key = [2]string{e.BID, e.AID}
		}
		if decayed[key] {
			continue
		}
		if err := store.DecayEdge(tx, e.AID, e.BID, delta); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("edge decay %s: %v", tuple, err))
			continue
		}
		decayed[key] = true
		result.EdgesDecayed++
	}
}

// tupleToMemoryID returns the memory ID for a body string.
// Returns "" if the body is empty.
func tupleToMemoryID(body string) string {
	if body == "" {
		return ""
	}
	return store.MemoryID(body)
}

// emit writes the result to stdout per the --format flag.
func emit(r consolidate.Result, format string) {
	switch format {
	case "human":
		fmt.Fprint(os.Stdout, consolidate.RenderHuman(r))
	case "json":
		fmt.Fprintln(os.Stdout, consolidate.RenderJSON(r))
	default: // both
		fmt.Fprint(os.Stdout, consolidate.RenderHuman(r))
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, consolidate.RenderJSON(r))
	}
}
