package main

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/aslanbrooke/jirahere/internal/settings"
)

const configureUsage = "usage: jirahere configure <set|get> ..."

const configureSetUsage = "usage: jirahere configure set [--project <key>] [--type <name>] [--current-quarter <label>] [--previous-quarter <label>] [--next-quarter <label>] [--profile <name>]"

const configureGetUsage = "usage: jirahere configure get [--json] [--profile <name>]"

var configureSetFieldLabels = map[string]string{
	"project":          "defaults.project",
	"type":             "defaults.issue_type",
	"current-quarter":  "defaults.current_quarter",
	"previous-quarter": "defaults.previous_quarter",
	"next-quarter":     "defaults.next_quarter",
}

func runConfigure(args []string) error {
	if hasHelpToken(args) {
		if len(args) > 0 && args[0] == "get" {
			return runConfigureGet([]string{"--help"})
		}
		return runConfigureSet([]string{"--help"})
	}
	if len(args) == 0 || (args[0] != "set" && args[0] != "get") {
		return usagef(configureUsage)
	}
	if args[0] == "get" {
		return runConfigureGet(args[1:])
	}
	return runConfigureSet(args[1:])
}

func runConfigureSet(args []string) error {
	fs := flag.NewFlagSet("configure set", flag.ContinueOnError)
	project := fs.String("project", "", "project key to store as settings.json's defaults.project")
	issueType := fs.String("type", "", "issue type name to store as settings.json's defaults.issue_type")
	currentQuarter := fs.String("current-quarter", "", "quarter label to store as settings.json's defaults.current_quarter (accepted verbatim, not validated)")
	previousQuarter := fs.String("previous-quarter", "", "quarter label to store as settings.json's defaults.previous_quarter (accepted verbatim, not validated)")
	nextQuarter := fs.String("next-quarter", "", "quarter label to store as settings.json's defaults.next_quarter (accepted verbatim, not validated)")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, args); err != nil {
		return err
	}
	prof, err := resolveProfile()
	if err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usagef(configureSetUsage)
	}

	type givenFlag struct {
		name  string
		value *string
	}
	var given []givenFlag
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "project":
			given = append(given, givenFlag{name: f.Name, value: project})
		case "type":
			given = append(given, givenFlag{name: f.Name, value: issueType})
		case "current-quarter":
			given = append(given, givenFlag{name: f.Name, value: currentQuarter})
		case "previous-quarter":
			given = append(given, givenFlag{name: f.Name, value: previousQuarter})
		case "next-quarter":
			given = append(given, givenFlag{name: f.Name, value: nextQuarter})
		}
	})
	if len(given) == 0 {
		return usagef(configureSetUsage)
	}
	for _, g := range given {
		if *g.value == "" {
			return usagef("jirahere: --%s was given but is empty; an empty value is not allowed.", g.name)
		}
	}

	s, err := settings.Load(prof)
	if err != nil {
		return failf("jirahere: could not load settings: %s.", err)
	}

	for _, g := range given {
		switch g.name {
		case "project":
			s.Defaults.Project = *g.value
		case "type":
			s.Defaults.IssueType = *g.value
		case "current-quarter":
			s.Defaults.CurrentQuarter = *g.value
		case "previous-quarter":
			s.Defaults.PreviousQuarter = *g.value
		case "next-quarter":
			s.Defaults.NextQuarter = *g.value
		}
	}

	if err := settings.Save(s, prof); err != nil {
		return failf("jirahere: could not save settings: %s.", err)
	}

	suffix := ""
	if prof != "" {
		suffix = fmt.Sprintf(" (profile %q)", sanitizeSingleLine(prof))
	}
	for _, g := range given {
		if _, err := fmt.Printf("%s set to %s%s.\n", configureSetFieldLabels[g.name], sanitizeSingleLine(*g.value), suffix); err != nil {
			return err
		}
	}
	return nil
}

type configureGetDoc struct {
	Project         *string `json:"project"`
	IssueType       *string `json:"issue_type"`
	CurrentQuarter  *string `json:"current_quarter"`
	PreviousQuarter *string `json:"previous_quarter"`
	NextQuarter     *string `json:"next_quarter"`
}

func runConfigureGet(args []string) error {
	fs := flag.NewFlagSet("configure get", flag.ContinueOnError)
	jsonOutput := fs.Bool("json", false, "render the defaults.* fields as one JSON object")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, args); err != nil {
		return err
	}
	prof, err := resolveProfile()
	if err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usagef(configureGetUsage)
	}

	s, err := settings.Load(prof)
	if err != nil {
		return failf("jirahere: could not load settings: %s.", err)
	}

	if *jsonOutput {
		return renderConfigureGetJSON(s.Defaults)
	}
	return renderConfigureGetText(s.Defaults)
}

func renderConfigureGetText(d settings.Defaults) error {
	fields := [5]struct{ label, value string }{
		{"project", d.Project},
		{"issue_type", d.IssueType},
		{"current_quarter", d.CurrentQuarter},
		{"previous_quarter", d.PreviousQuarter},
		{"next_quarter", d.NextQuarter},
	}
	for _, f := range fields {
		value := "(unset)"
		if f.value != "" {
			value = sanitizeSingleLine(f.value)
		}
		if _, err := fmt.Printf("%s: %s\n", f.label, value); err != nil {
			return err
		}
	}
	return nil
}

func renderConfigureGetJSON(d settings.Defaults) error {
	var doc configureGetDoc
	if d.Project != "" {
		doc.Project = &d.Project
	}
	if d.IssueType != "" {
		doc.IssueType = &d.IssueType
	}
	if d.CurrentQuarter != "" {
		doc.CurrentQuarter = &d.CurrentQuarter
	}
	if d.PreviousQuarter != "" {
		doc.PreviousQuarter = &d.PreviousQuarter
	}
	if d.NextQuarter != "" {
		doc.NextQuarter = &d.NextQuarter
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	_, err := fmt.Println(string(b))
	return err
}
