package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aslanbrooke/jirahere/internal/command"
	"github.com/aslanbrooke/jirahere/internal/skills"
)

var skillsInstallValueFlags = map[string]bool{"dir": true}

var skillsInstallBoolFlags = map[string]bool{"prune": true}

var listEmbeddedSkills = skills.List

const skillsInstallUsage = "usage: jirahere skills install <app> [<agent-name>] [--dir <path>] [--prune]"

var supportedSkillsInstallAgents = map[string]bool{
	"jirasistant-req": true,
}

type skillsInstallOptions struct {
	dir    string
	dirSet bool
	agent  string
	prune  bool
}

func selectSkillsForAgent(agentName string, all []skills.Skill) ([]skills.Skill, error) {
	if agentName == "" {
		return skills.DefaultSet(all), nil
	}
	return skills.JirasistantReqSet(all)
}

func splitSkillsInstallArgs(args []string) ([]string, []string) {
	var positional, flagArgs []string
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
		if (skillsInstallValueFlags[name] || !isSkillsInstallFlag(name)) && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	return positional, flagArgs
}

func isSkillsInstallFlag(name string) bool {
	return skillsInstallValueFlags[name] || skillsInstallBoolFlags[name]
}

func runSkills(args []string) error {
	if hasHelpToken(args) {

		return runSkillsInstall([]string{"--help"})
	}
	if len(args) == 0 || args[0] != "install" {
		return usagef(skillsInstallUsage)
	}
	return runSkillsInstall(args[1:])
}

func runSkillsInstall(args []string) error {
	app, opts, err := parseSkillsInstallOptions(args)
	if err != nil {
		return err
	}
	var targetDir *string
	if opts.dirSet {
		targetDir = &opts.dir
	}
	target, err := command.ResolveSkillsInstallTarget(app, targetDir)
	if err != nil {
		if errors.Is(err, command.ErrSkillsTargetUnsafe) {
			return failf("jirahere: skills install: could not resolve target directory")
		}
		return failf("jirahere: skills install: could not resolve home directory")
	}
	bundled, err := listEmbeddedSkills()
	if err != nil {
		return failf("jirahere: skills install: could not load embedded skills")
	}
	selected, err := selectSkillsForAgent(opts.agent, bundled)
	if err != nil {
		return failf("jirahere: skills install: %s", err.Error())
	}
	result, err := command.InstallSkills(target, selected, opts.prune)
	for _, event := range result.Events {
		if _, writeErr := fmt.Fprintf(os.Stdout, "%s %s\n", event.Action, sanitizeSingleLine(event.Name)); writeErr != nil {
			return writeErr
		}
	}
	if err != nil {
		var installErr *command.SkillsInstallError
		if errors.As(err, &installErr) {
			return failf("jirahere: skills install: failed to write skill %s", installErr.Skill)
		}
		return failf("jirahere: skills install: could not prepare target directory")
	}
	if _, err := fmt.Fprintf(os.Stdout, "installed %d skill(s): %d written, %d replaced, %d pruned\n", len(result.Written)+len(result.Replaced), len(result.Written), len(result.Replaced), len(result.Pruned)); err != nil {
		return err
	}
	return nil
}

func parseSkillsInstallOptions(args []string) (string, skillsInstallOptions, error) {
	positional, flagArgs := splitSkillsInstallArgs(args)
	help := hasHelpToken(args)
	if help {
		flagArgs = []string{"--help"}
	}
	if !help && (len(positional) < 1 || len(positional) > 2) {
		return "", skillsInstallOptions{}, usagef(skillsInstallUsage)
	}
	app := ""
	if len(positional) > 0 {
		app = positional[0]
	}
	if !help && app != "codex" && app != "claude" {
		return "", skillsInstallOptions{}, usagef(skillsInstallUsage)
	}
	agentName := ""
	if len(positional) > 1 {
		agentName = positional[1]
	}
	if !help && agentName != "" && !supportedSkillsInstallAgents[agentName] {
		return "", skillsInstallOptions{}, usagef(skillsInstallUsage)
	}

	fs := flag.NewFlagSet("skills install", flag.ContinueOnError)
	var dir string
	dirCounter := &repeatCounter{target: &dir}
	fs.Var(dirCounter, "dir", "target skills root (overrides the app default)")
	var prune bool
	fs.BoolVar(&prune, "prune", false, "delete owned-prefix skill directories not in the shipped set")
	if err := parseFlagSet(fs, flagArgs, flagUsageSpec{Positionals: []string{"<app>", "[<agent-name>]"}}); err != nil {
		return "", skillsInstallOptions{}, err
	}
	if dirCounter.count > 1 {
		return "", skillsInstallOptions{}, usagef(skillsInstallUsage)
	}

	return app, skillsInstallOptions{dir: dir, dirSet: dirCounter.count == 1, agent: agentName, prune: prune}, nil
}
