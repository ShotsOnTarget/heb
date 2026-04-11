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
		fmt.Fprintln(os.Stderr, "  recall       retrieve memories (Contract 2 JSON on stdin)")
		fmt.Fprintln(os.Stderr, "  consolidate  apply session learning (Contract 4 JSON on stdin)")
		fmt.Fprintln(os.Stderr, "  session      durable pipeline session state")
		fmt.Fprintln(os.Stderr, "  status       graph health and statistics")
		fmt.Fprintln(os.Stderr, "  dream        dream subcommands (seeds, random-pairs, write, pairs)")
		fmt.Fprintln(os.Stderr, "  purge        delete memories by ID")
		fmt.Fprintln(os.Stderr, "  claude       manage claude commands (install, update, status)")
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
	case "claude":
		os.Exit(runClaude(os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "heb: unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}
