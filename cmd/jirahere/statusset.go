package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/jira"
)

const statusSetUsage = "usage: jirahere status set <id> --status <name> [--profile <name>]"

var statusSetValueFlags = map[string]bool{"status": true, "profile": true}

func splitStatusSetArgs(args []string) (positional []string, flagArgs []string) {
	return splitPositionalFlagArgs(args, statusSetValueFlags)
}

func splitPositionalFlagArgs(args []string, valueFlags map[string]bool) (positional []string, flagArgs []string) {
	terminated := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if terminated {
			positional = append(positional, a)
			continue
		}
		if a == "--" {
			terminated = true
			continue
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}
		flagArgs = append(flagArgs, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if valueFlags[name] && i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	return positional, flagArgs
}

func runStatusSet(args []string) error {
	var positional []string
	var flagArgs []string
	if hasHelpToken(args) {
		flagArgs = []string{"--help"}
	} else {
		positional, flagArgs = splitStatusSetArgs(args)
	}

	fs := flag.NewFlagSet("status set", flag.ContinueOnError)
	status := fs.String("status", "", "target status name to transition <id> to (required, non-empty; matched exactly, case-sensitively, against one of <id>'s currently available transitions)")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, flagArgs, flagUsageSpec{Positionals: []string{"<id>"}}); err != nil {
		return err
	}
	if fs.NArg() != 0 || len(positional) != 1 || positional[0] == "" {
		return usagef(statusSetUsage)
	}
	prof, err := resolveProfile()
	if err != nil {
		return err
	}
	if *status == "" {
		return usagef("jirahere: --status is required and must not be empty.")
	}
	id := positional[0]

	provider, err := auth.LoadProvider(prof)
	if err != nil {
		if errors.Is(err, auth.ErrNotLoggedIn) {
			return failf("%s", notLoggedInMessage(prof))
		}
		return failf("jirahere: could not load Jira credentials. Run \"%s\" again.", loginRemediation(prof))
	}
	ctx := context.Background()
	creds, err := provider.Credentials(ctx)
	if err != nil {
		return refreshCredentialsFailureFor(err, prof)
	}
	var client jira.TransitionService = jira.NewClient(creds.BaseURL, creds.AuthHeader)

	if err := transitionByName(ctx, client, "status set", id, *status); err != nil {
		return err
	}

	_, err = fmt.Printf("Status set: %s -> %s\n", sanitizeSingleLine(id), sanitizeSingleLine(*status))
	return err
}

func availableStatusesClause(transitions []jira.Transition) string {
	names := availableStatusNames(transitions)
	if len(names) == 0 {
		return "no transitions are currently available."
	}
	return fmt.Sprintf("available: %s.", strings.Join(names, ", "))
}

func availableStatusNames(transitions []jira.Transition) []string {
	names := make([]string, 0, len(transitions))
	seen := make(map[string]struct{}, len(transitions))
	for _, t := range transitions {
		if _, ok := seen[t.ToStatusName]; ok {
			continue
		}
		seen[t.ToStatusName] = struct{}{}
		names = append(names, t.ToStatusName)
	}
	return names
}

func transitionByName(ctx context.Context, client jira.TransitionService, cmd, key, name string) error {
	transitions, err := client.Transitions(ctx, key)
	if err != nil {
		return failf("jirahere: %s could not list available transitions for %q: %s.", cmd, key, renderJiraError(err))
	}
	var matches []jira.Transition
	for _, t := range transitions {
		if t.ToStatusName == name {
			matches = append(matches, t)
		}
	}
	switch len(matches) {
	case 0:
		return failf("jirahere: %s: %q is not an available status for %q; %s", cmd, name, key, availableStatusesClause(transitions))
	case 1:

	default:
		return failf("jirahere: %s: %q is ambiguous for %q (%d matching transitions); no transition was made.", cmd, name, key, len(matches))
	}
	if err := client.DoTransition(ctx, key, matches[0].ID); err != nil {
		return failf("jirahere: %s could not transition %q to %q: %s.", cmd, key, name, renderJiraError(err))
	}
	return nil
}
