package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: heb <command> [args]")
		fmt.Fprintln(os.Stderr, "commands:")
		fmt.Fprintln(os.Stderr, "  init         initialise memory store in current repo")
		fmt.Fprintln(os.Stderr, "  sense        parse a prompt into tokens via LLM")
		fmt.Fprintln(os.Stderr, "  retrieve     sense + retrieve context from memory/git/beads")
		fmt.Fprintln(os.Stderr, "  reflect      sense + retrieve + reconcile memories against prompt")
		fmt.Fprintln(os.Stderr, "  recall       retrieve memories (contract:sense>recall JSON on stdin)")
		fmt.Fprintln(os.Stderr, "  consolidate  apply session learning (contract:learn>consolidate JSON on stdin)")
		fmt.Fprintln(os.Stderr, "  session      durable pipeline session state")
		fmt.Fprintln(os.Stderr, "  status       graph health and statistics")
		fmt.Fprintln(os.Stderr, "  dream        dream subcommands (seeds, random-pairs, write, pairs)")
		fmt.Fprintln(os.Stderr, "  purge        delete memories by ID")
		fmt.Fprintln(os.Stderr, "  resume       continue an open session with a new prompt via claude --resume")
		fmt.Fprintln(os.Stderr, "  learn        extract lessons from a session's transcript via LLM")
		fmt.Fprintln(os.Stderr, "  remember     learn + consolidate + close session in one step")
		fmt.Fprintln(os.Stderr, "  config       get/set configuration (verbosity: loud|quiet|mute)")
		fmt.Fprintln(os.Stderr, "  claude       manage claude commands (install, update, status)")
		fmt.Fprintln(os.Stderr, "  gui          launch the desktop interface")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "init":
		os.Exit(runInit(os.Args[2:]))
	case "recall":
		os.Exit(runRecall(os.Args[2:]))
	case "consolidate":
		os.Exit(runConsolidate(os.Args[2:]))
	case "session":
		os.Exit(runSession(os.Args[2:]))
	case "status":
		os.Exit(runStatus(os.Args[2:]))
	case "dream":
		os.Exit(runDream(os.Args[2:]))
	case "purge":
		os.Exit(runPurge(os.Args[2:]))
	case "config":
		os.Exit(runConfig(os.Args[2:]))
	case "sense":
		os.Exit(runSense(os.Args[2:]))
	case "retrieve":
		os.Exit(runRetrieve(os.Args[2:]))
	case "reflect":
		os.Exit(runReflect(os.Args[2:]))
	case "resume":
		os.Exit(runResume(os.Args[2:]))
	case "learn":
		os.Exit(runLearn(os.Args[2:]))
	case "remember":
		os.Exit(runRemember(os.Args[2:]))
	case "claude":
		os.Exit(runClaude(os.Args[2:]))
	case "projects":
		os.Exit(runProjects(os.Args[2:]))
	case "gui":
		os.Exit(runGUI(os.Args[2:]))
	case "build":
		os.Exit(runBuild(os.Args[2:]))
	case "version":
		fmt.Println("heb " + Version)
		os.Exit(0)
	default:
		// Not a subcommand — treat entire args as a prompt
		os.Exit(runPipeline(os.Args[1:]))
	}
}
