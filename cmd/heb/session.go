package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/steelboltgames/heb/internal/store"
)

func runSession(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: heb session <start|write|read|list|resume|close|trash> [args]")
		return 2
	}
	switch args[0] {
	case "start":
		return runSessionStart(args[1:])
	case "write":
		return runSessionWrite(args[1:])
	case "read":
		return runSessionRead(args[1:])
	case "list":
		return runSessionList(args[1:])
	case "resume":
		return runSessionResume(args[1:])
	case "close":
		return runSessionClose(args[1:])
	case "trash":
		return runSessionTrash(args[1:])
	case "chat-save":
		return runSessionChatSave(args[1:])
	case "chat-list":
		return runSessionChatList(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "heb session: unknown subcommand %q\n", args[0])
		return 2
	}
}

// runSessionStart reads a sense contract from stdin, extracts session_id
// and project, creates the session, and writes the sense contract.
// Prints the session_id to stdout.
func runSessionStart(_ []string) int {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session start: read stdin: %v\n", err)
		return 1
	}

	// Extract session_id and project from the sense contract
	var sense struct {
		SessionID string `json:"session_id"`
		Project   string `json:"project"`
	}
	if err := json.Unmarshal(data, &sense); err != nil {
		fmt.Fprintf(os.Stderr, "heb session start: parse json: %v\n", err)
		return 1
	}
	if sense.SessionID == "" {
		fmt.Fprintln(os.Stderr, "heb session start: session_id required in sense contract")
		return 1
	}
	if sense.Project == "" {
		fmt.Fprintln(os.Stderr, "heb session start: project required in sense contract")
		return 1
	}

	s, err := openStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session start: %v\n", err)
		return 1
	}
	defer s.Close()

	if err := store.StartSession(s.DB(), sense.SessionID, sense.Project, string(data)); err != nil {
		fmt.Fprintf(os.Stderr, "heb session start: %v\n", err)
		return 1
	}

	fmt.Fprintln(os.Stdout, sense.SessionID)
	fmt.Fprintf(os.Stderr, "session started: %s\n", sense.SessionID)
	return 0
}

// runSessionWrite reads a contract from stdin and writes it to the
// specified session and step.
// Usage: heb session write <session_id> <step>
func runSessionWrite(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: heb session write <session_id> <step>")
		return 2
	}
	sessionID := args[0]
	step := args[1]

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session write: read stdin: %v\n", err)
		return 1
	}

	s, err := openStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session write: %v\n", err)
		return 1
	}
	defer s.Close()

	if err := store.WriteContract(s.DB(), sessionID, step, string(data)); err != nil {
		fmt.Fprintf(os.Stderr, "heb session write: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "wrote %s contract for session %s\n", step, sessionID)
	return 0
}

// runSessionRead outputs a contract from the specified session and step.
// Usage: heb session read <session_id> <step>
func runSessionRead(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: heb session read <session_id> <step>")
		return 2
	}
	sessionID := args[0]
	step := args[1]

	s, err := openStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session read: %v\n", err)
		return 1
	}
	defer s.Close()

	contract, err := store.ReadContract(s.DB(), sessionID, step)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session read: %v\n", err)
		return 1
	}

	fmt.Fprint(os.Stdout, contract)
	return 0
}

// runSessionList shows recent sessions with their status and steps.
func runSessionList(_ []string) int {
	s, err := openStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session list: %v\n", err)
		return 1
	}
	defer s.Close()

	sessions, err := store.ListSessions(s.DB(), 10)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session list: %v\n", err)
		return 1
	}

	if len(sessions) == 0 {
		fmt.Fprintln(os.Stderr, "no sessions found")
		return 0
	}

	fmt.Fprintln(os.Stdout, "SESSIONS")
	fmt.Fprintln(os.Stdout, "────────────────────────────────────────")
	for _, sess := range sessions {
		age := time.Since(time.Unix(sess.CreatedAt, 0)).Truncate(time.Minute)
		steps := "—"
		if len(sess.Steps) > 0 {
			steps = strings.Join(sess.Steps, " → ")
		}
		fmt.Fprintf(os.Stdout, "%-8s  %-28s  %-12s  %s  (%s ago)\n",
			sess.Status, sess.ID, sess.Project, steps, age)
	}
	fmt.Fprintln(os.Stdout, "────────────────────────────────────────")
	return 0
}

// runSessionResume shows the status of a specific session.
// Usage: heb session resume <session_id>
func runSessionResume(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: heb session resume <session_id>")
		return 2
	}
	sessionID := args[0]

	s, err := openStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session resume: %v\n", err)
		return 1
	}
	defer s.Close()

	sess, next, err := store.ResumeSession(s.DB(), sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session resume: %v\n", err)
		return 1
	}

	fmt.Fprintln(os.Stdout, "SESSION RESUME")
	fmt.Fprintln(os.Stdout, "────────────────────────────────────────")
	fmt.Fprintf(os.Stdout, "id:       %s\n", sess.ID)
	fmt.Fprintf(os.Stdout, "project:  %s\n", sess.Project)
	fmt.Fprintf(os.Stdout, "status:   %s\n", sess.Status)
	fmt.Fprintf(os.Stdout, "created:  %s\n", time.Unix(sess.CreatedAt, 0).UTC().Format(time.RFC3339))

	fmt.Fprintf(os.Stdout, "\ncompleted steps:\n")
	if len(sess.Steps) == 0 {
		fmt.Fprintln(os.Stdout, "  (none)")
	} else {
		for _, step := range sess.Steps {
			fmt.Fprintf(os.Stdout, "  ✓ %s\n", step)
		}
	}

	fmt.Fprintf(os.Stdout, "\nmissing steps:\n")
	done := make(map[string]bool)
	for _, step := range sess.Steps {
		done[step] = true
	}
	hasMissing := false
	for _, step := range store.ValidSteps {
		if !done[step] {
			fmt.Fprintf(os.Stdout, "  ○ %s\n", step)
			hasMissing = true
		}
	}
	if !hasMissing {
		fmt.Fprintln(os.Stdout, "  (all complete)")
	}

	if next != "" {
		fmt.Fprintf(os.Stdout, "\nnext step: %s\n", next)
	} else if sess.Status == "active" {
		fmt.Fprintln(os.Stdout, "\nall steps complete — run: heb session close", sess.ID)
	}
	fmt.Fprintln(os.Stdout, "────────────────────────────────────────")
	return 0
}

// runSessionClose marks a session as complete.
// Usage: heb session close <session_id>
func runSessionClose(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: heb session close <session_id>")
		return 2
	}
	sessionID := args[0]

	s, err := openStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session close: %v\n", err)
		return 1
	}
	defer s.Close()

	if err := store.CloseSession(s.DB(), sessionID); err != nil {
		fmt.Fprintf(os.Stderr, "heb session close: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "session closed: %s\n", sessionID)
	return 0
}

// runSessionTrash discards a session without learning.
// Usage: heb session trash <session_id>
func runSessionTrash(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: heb session trash <session_id>")
		return 2
	}
	sessionID := args[0]

	s, err := openStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session trash: %v\n", err)
		return 1
	}
	defer s.Close()

	if err := store.TrashSession(s.DB(), sessionID); err != nil {
		fmt.Fprintf(os.Stderr, "heb session trash: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "session trashed: %s\n", sessionID)
	return 0
}

// runSessionChatSave clears and re-writes all GUI chat messages for a session.
// Usage: heb session chat-save <session_id>
// Reads JSON array from stdin: [{"role":"...", "content":"...", "phase":"..."},...]
func runSessionChatSave(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: heb session chat-save <session_id>")
		return 2
	}
	sessionID := args[0]

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session chat-save: read stdin: %v\n", err)
		return 1
	}

	var msgs []struct {
		Role    string  `json:"role"`
		Content string  `json:"content"`
		Phase   *string `json:"phase,omitempty"`
	}
	if err := json.Unmarshal(data, &msgs); err != nil {
		fmt.Fprintf(os.Stderr, "heb session chat-save: parse json: %v\n", err)
		return 1
	}

	s, err := openStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session chat-save: %v\n", err)
		return 1
	}
	defer s.Close()

	// Clear existing chat log for this session
	if err := store.ClearGUIChat(s.DB(), sessionID); err != nil {
		fmt.Fprintf(os.Stderr, "heb session chat-save: clear: %v\n", err)
		return 1
	}

	// Write all messages
	for _, msg := range msgs {
		if _, err := store.WriteGUIChat(s.DB(), sessionID, msg.Role, msg.Content, msg.Phase); err != nil {
			fmt.Fprintf(os.Stderr, "heb session chat-save: write: %v\n", err)
			return 1
		}
	}

	fmt.Fprintf(os.Stderr, "saved %d chat messages for session %s\n", len(msgs), sessionID)
	return 0
}

// runSessionChatList returns all GUI chat messages for a session as JSON.
// Usage: heb session chat-list <session_id>
func runSessionChatList(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: heb session chat-list <session_id>")
		return 2
	}
	sessionID := args[0]

	s, err := openStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session chat-list: %v\n", err)
		return 1
	}
	defer s.Close()

	entries, err := store.ListGUIChat(s.DB(), sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session chat-list: %v\n", err)
		return 1
	}

	out, err := json.Marshal(entries)
	if err != nil {
		fmt.Fprintf(os.Stderr, "heb session chat-list: marshal: %v\n", err)
		return 1
	}
	fmt.Fprint(os.Stdout, string(out))
	return 0
}

// openStore is a shared helper — opens the global heb store.
func openStore() (*store.SQLiteStore, error) {
	return store.Open()
}
