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
		if statusSetValueFlags[name] && i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	return positional, flagArgs
}

func runStatusSet(args []string) error {
	positional, flagArgs := splitStatusSetArgs(args)

	fs := flag.NewFlagSet("status set", flag.ContinueOnError)
	status := fs.String("status", "", "target status name to transition <id> to (required, non-empty; matched exactly, case-sensitively, against one of <id>'s currently available transitions)")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, flagArgs); err != nil {
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

	transitions, err := client.Transitions(ctx, id)
	if err != nil {
		return failf("jirahere: status set could not list available transitions for %q: %s.", id, renderJiraError(err))
	}

	var matches []jira.Transition
	for _, t := range transitions {
		if t.ToStatusName == *status {
			matches = append(matches, t)
		}
	}

	switch len(matches) {
	case 0:
		return failf("jirahere: status set: %q is not an available status for %q; %s", *status, id, availableStatusesClause(transitions))
	case 1:

	default:
		return failf("jirahere: status set: %q is ambiguous for %q (%d matching transitions); no transition was made.", *status, id, len(matches))
	}

	if err := client.DoTransition(ctx, id, matches[0].ID); err != nil {
		return failf("jirahere: status set could not transition %q to %q: %s.", id, *status, renderJiraError(err))
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
