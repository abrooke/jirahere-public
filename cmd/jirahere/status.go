package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"

	"github.com/aslanbrooke/jirahere/internal/auth"
)

const statusUsage = "usage: jirahere status [--json] [--profile <name>]\n       jirahere status set <id> --status <name> [--profile <name>]"

const statusRejectUsage = `usage: jirahere status [--json] [--profile <name>] (see "jirahere status --help" for status set)`

type statusDoc struct {
	LoggedIn bool    `json:"logged_in"`
	Site     *string `json:"site"`
	Provider *string `json:"provider"`
}

func runStatus(args []string) error {
	if hasHelpToken(args) {
		if _, err := fmt.Println(statusUsage); err != nil {
			return err
		}
		return flag.ErrHelp
	}
	if len(args) > 0 && args[0] == "set" {
		return runStatusSet(args[1:])
	}
	return runStatusCheck(args)
}

func runStatusCheck(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	jsonOutput := fs.Bool("json", false, "print the login state as a JSON object (a not-logged-in state still exits 0)")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usagef(statusRejectUsage)
	}

	prof, err := resolveProfile()
	if err != nil {
		return err
	}

	if _, err := auth.LoadProvider(prof); err != nil {
		if errors.Is(err, auth.ErrNotLoggedIn) {
			return renderStatus(*jsonOutput, false, "", "", prof)
		}
		return statusUnreadableFailure(prof)
	}

	cfg, err := auth.Load(prof)
	if err != nil {
		return statusUnreadableFailure(prof)
	}
	return renderStatus(*jsonOutput, true, cfg.Site, cfg.Provider, prof)
}

func statusUnreadableFailure(prof string) error {
	return failf("jirahere: cannot determine login state: %s is unreadable, malformed, or holds an invalid site.", profileConfigPathDisplay(prof))
}

func renderStatus(jsonOutput, loggedIn bool, site, provider, prof string) error {
	if jsonOutput {
		return renderStatusJSON(loggedIn, site, provider)
	}
	return renderStatusText(loggedIn, site, provider, prof)
}

func renderStatusText(loggedIn bool, site, provider, prof string) error {
	if !loggedIn {
		_, err := fmt.Println(`Not logged in. Run "` + loginRemediation(prof) + `" first.`)
		return err
	}
	_, err := fmt.Printf(
		"Logged in to %s via %s%s.\n",
		sanitizeSingleLine(site),
		sanitizeSingleLine(auth.ProviderLabel(provider)),
		profileSuffix(prof),
	)
	return err
}

func renderStatusJSON(loggedIn bool, site, provider string) error {
	doc := statusDoc{LoggedIn: loggedIn}
	if loggedIn {
		doc.Site = &site
		doc.Provider = &provider
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	_, err := fmt.Println(string(b))
	return err
}
