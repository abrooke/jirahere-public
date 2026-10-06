package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

const exitUsage = 2

func main() {
	if len(os.Args) < 2 {
		if err := usage(os.Stderr); err != nil {
			fmt.Fprintf(os.Stderr, "jirahere: %s\n", err)
		}
		os.Exit(exitUsage)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "login":
		err = runLogin(args)
	case "logout":
		err = runLogout(args)
	case "status":
		err = runStatus(args)
	case "clone":
		err = runClone(args)
	case "create":
		err = runCreate(args)
	case "move":
		err = runMove(args)
	case "rehome-children":
		err = runRehomeChildren(args)
	case "context":
		err = runContext(args)
	case "skills":
		err = runSkills(args)
	case "agent":
		err = runAgent(args)
	case "quarter":
		err = runQuarter(args)
	case "cache":
		err = runCache(args)
	case "configure":
		err = runConfigure(args)
	case "comment":
		err = runComment(args)
	case "vision":
		err = runVision(args)
	case "-h", "--help", "help":
		if err := usage(os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "jirahere: %s\n", err)
			os.Exit(1)
		}
		return
	default:
		fmt.Fprintf(os.Stderr, "jirahere: command %q not implemented yet\n", cmd)
		os.Exit(exitUsage)
	}

	if err != nil {
		if errors.Is(err, flag.ErrHelp) {

			return
		}
		var usageErr *errUsage
		if errors.As(err, &usageErr) {

			os.Exit(exitUsage)
		}
		var silent *errSilent
		if errors.As(err, &silent) {

			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "jirahere: %s\n", err)
		os.Exit(1)
	}
}

const maxListDescriptionRunes = 60

func usage(w io.Writer) error {
	lines := []string{
		"usage: jirahere <command> [args]",
		"",
		"commands:",
		"  login            authenticate to Jira and store credentials",
		"  logout           remove stored credentials",
		"  status           report whether Jira credentials are stored (no network)",
		"  status set       transition a work item to a named workflow status",
		"  clone            clone a work item, linking the new item back to its source",
		"  create           create a work item from directly supplied fields",
		"  move             reparent/rewrite a work item and its descendants in place",
		"  rehome-children  reparent every direct child of one work item to another",
		"  context get      assemble hierarchy context for a work item",
		"  skills install   install a jirahere-* skill set; --prune removes stale ones",
		"  agent install    install a bundled agent persona for codex or claude",
		"  quarter list     list a quarter's work items from the cached inventory",
		"  cache clear      drop the cached quarter inventory",
		"  configure set    write settings.json defaults.* fields",
		"  configure get    print settings.json defaults.* fields",
		"  comment add      add a plain-text comment to a work item",
		"  comment list     list a work item's comments in Jira's returned order",
		"  vision           show the current quarter's active Vision item",
	}
	for _, l := range lines {
		if _, err := fmt.Fprintln(w, l); err != nil {
			return err
		}
	}
	return nil
}
