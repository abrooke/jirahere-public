package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aslanbrooke/jirahere/internal/agent"
	"github.com/aslanbrooke/jirahere/internal/command"
)

var loadAgentAsset = agent.Asset

var agentInstallValueFlags = map[string]bool{"dir": true}

const agentInstallUsage = "usage: jirahere agent install <app> [<agent>] [--dir <path>]"

var supportedInstallAgents = map[string]bool{
	"jirasistant":     true,
	"jirasistant-req": true,
}

type agentInstallOptions struct {
	dir    string
	dirSet bool
	agent  string
}

func splitAgentInstallArgs(args []string) ([]string, []string) {
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
		if (agentInstallValueFlags[name] || !isAgentInstallFlag(name)) && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	return positional, flagArgs
}

func isAgentInstallFlag(name string) bool {
	return agentInstallValueFlags[name]
}

func runAgent(args []string) error {
	if hasHelpToken(args) {

		return runAgentInstall([]string{"--help"})
	}
	if len(args) == 0 || args[0] != "install" {
		return usagef(agentInstallUsage)
	}
	return runAgentInstall(args[1:])
}

func runAgentInstall(args []string) error {
	app, opts, err := parseAgentInstallOptions(args)
	if err != nil {
		return err
	}
	var targetDir *string
	if opts.dirSet {
		targetDir = &opts.dir
	}
	target, err := command.ResolveAgentInstallTarget(opts.agent, app, targetDir)
	if err != nil {
		if errors.Is(err, command.ErrAgentTargetUnsafe) {
			return failf("jirahere: agent install: could not resolve target path")
		}
		return failf("jirahere: agent install: could not resolve home directory")
	}
	_, contents, err := loadAgentAsset(opts.agent, app)
	if err != nil {
		return failf("jirahere: agent install: could not load embedded agent file")
	}
	replaced, err := command.InstallAgentFile(target, contents)
	if err != nil {
		if errors.Is(err, command.ErrAgentPrepareTarget) {
			return failf("jirahere: agent install: could not prepare target directory")
		}
		return failf("jirahere: agent install: failed to write agent file")
	}
	action := "written"
	if replaced {
		action = "replaced"
	}
	_, err = fmt.Fprintf(os.Stdout, "%s %s\n", action, opts.agent)
	return err
}

func parseAgentInstallOptions(args []string) (string, agentInstallOptions, error) {
	positional, flagArgs := splitAgentInstallArgs(args)
	help := hasHelpToken(args)
	if help {
		flagArgs = []string{"--help"}
	}
	if !help && (len(positional) < 1 || len(positional) > 2) {
		return "", agentInstallOptions{}, usagef(agentInstallUsage)
	}
	app := ""
	if len(positional) > 0 {
		app = positional[0]
	}
	if !help && app != "codex" && app != "claude" {
		return "", agentInstallOptions{}, usagef(agentInstallUsage)
	}
	agentName := "jirasistant"
	if len(positional) > 1 {
		agentName = positional[1]
	}
	if !help && !supportedInstallAgents[agentName] {
		return "", agentInstallOptions{}, usagef(agentInstallUsage)
	}

	fs := flag.NewFlagSet("agent install", flag.ContinueOnError)
	var dir string
	dirCounter := &repeatCounter{target: &dir}
	fs.Var(dirCounter, "dir", "target agent root; the app's agent filename is appended to it (overrides the app default)")
	if err := parseFlagSet(fs, flagArgs, flagUsageSpec{Positionals: []string{"<app>", "[<agent>]"}}); err != nil {
		return "", agentInstallOptions{}, err
	}
	if dirCounter.count > 1 {
		return "", agentInstallOptions{}, usagef(agentInstallUsage)
	}

	return app, agentInstallOptions{dir: dir, dirSet: dirCounter.count == 1, agent: agentName}, nil
}
