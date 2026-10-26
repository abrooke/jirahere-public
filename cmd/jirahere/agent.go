package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aslanbrooke/jirahere/internal/agent"
	"github.com/aslanbrooke/jirahere/internal/command"
)

var loadAgentAsset = agent.Asset

var agentInstallValueFlags = map[string]bool{"dir": true, "specialization": true}

const agentInstallUsage = "usage: jirahere agent install <app> [<agent>] [--dir <path>] [--specialization <file>]..."

var supportedInstallAgents = map[string]bool{
	"jirasistant":     true,
	"jirasistant-req": true,
}

type agentInstallOptions struct {
	dir             string
	dirSet          bool
	agent           string
	specializations []string
}

type specializationListFlag []string

func (s *specializationListFlag) String() string {
	if s == nil {
		return ""
	}
	return strings.Join(*s, ",")
}

func (s *specializationListFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func validateSpecialization(path string) error {
	if path == "" {
		return usagef("jirahere: agent install: --specialization requires a non-empty file path")
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return failf("jirahere: agent install: specialization file not found")
		}
		return failf("jirahere: agent install: specialization file is not readable")
	}
	if !info.Mode().IsRegular() {
		return failf("jirahere: agent install: specialization is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return failf("jirahere: agent install: specialization file is not readable")
	}
	_ = f.Close()
	return nil
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
	var stems []string
	for _, spec := range opts.specializations {
		stems = append(stems, specializationStem(app, spec))
	}
	target, err := command.ResolveAgentInstallTarget(opts.agent, app, targetDir, stems...)
	if err != nil {
		switch {
		case errors.Is(err, command.ErrAgentSpecializationStem):
			return failf("jirahere: agent install: specialization file name is not a valid stem (allowed: A-Z a-z 0-9 . _ -; combined filename at most 255 bytes)")
		case errors.Is(err, command.ErrAgentTargetUnsafe):
			return failf("jirahere: agent install: could not resolve target path")
		}
		return failf("jirahere: agent install: could not resolve home directory")
	}
	_, contents, err := loadAgentAsset(opts.agent, app)
	if err != nil {
		return failf("jirahere: agent install: could not load embedded agent file")
	}
	if len(opts.specializations) > 0 {
		specs := make([][]byte, 0, len(opts.specializations))
		for _, path := range opts.specializations {
			data, err := os.ReadFile(path)
			if err != nil {
				return failf("jirahere: agent install: specialization file is not readable")
			}
			specs = append(specs, data)
		}
		if app == "codex" {
			contents, err = agent.MergeCodex(contents, specs...)
			switch {
			case errors.Is(err, agent.ErrCodexEmbeddedFormat):
				return failf("jirahere: agent install: embedded agent file could not be merged (internal error)")
			case errors.Is(err, agent.ErrCodexSpecializationSecurity):
				return failf("jirahere: agent install: specialization has a sandbox_mode, approval_policy or web_search setting that cannot be merged (see the Codex config reference for supported values)")
			case err != nil:
				return failf("jirahere: agent install: specialization is not a valid Codex agent config (needs valid TOML and a non-empty developer_instructions string)")
			}
		} else {
			contents, err = agent.MergeClaude(contents, specs...)
			if errors.Is(err, agent.ErrEmbeddedFormat) {
				return failf("jirahere: agent install: embedded agent file could not be merged (internal error)")
			}
			var unsupported *agent.UnsupportedConstructError
			if errors.As(err, &unsupported) {
				return failf("jirahere: agent install: %v; supported frontmatter is plain \"key: value\" lines with optional double quotes (only \\\" and \\\\ escapes)", unsupported)
			}
			if err != nil {
				return failf("jirahere: agent install: specialization is not a valid Claude agent file (needs --- frontmatter, a non-empty body and a valid tools list)")
			}
		}
	}
	replaced, err := command.InstallAgentFile(target, contents)
	if err != nil {
		if errors.Is(err, command.ErrAgentPrepareTarget) {
			return failf("jirahere: agent install: could not prepare target directory")
		}
		return failf("jirahere: agent install: failed to write agent file")
	}
	installed := opts.agent
	if len(stems) > 0 {
		installed += "-" + strings.Join(stems, "-")
	}
	action := "written"
	if replaced {
		action = "replaced"
	}
	_, err = fmt.Fprintf(os.Stdout, "%s %s\n", action, installed)
	return err
}

func specializationStem(app, path string) string {
	base := filepath.Base(path)
	if app != "codex" {
		return strings.TrimSuffix(base, ".md")
	}
	if s, ok := strings.CutSuffix(base, ".config.toml"); ok {
		return s
	}
	return strings.TrimSuffix(base, ".toml")
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
	var specs specializationListFlag
	fs.Var(&specs, "specialization", "agent file to merge into the installed agent; repeatable, applied in the order given (each must be an existing, readable regular file)")
	if err := parseFlagSet(fs, flagArgs, flagUsageSpec{Positionals: []string{"<app>", "[<agent>]"}}); err != nil {
		return "", agentInstallOptions{}, err
	}
	if dirCounter.count > 1 {
		return "", agentInstallOptions{}, usagef(agentInstallUsage)
	}

	if !help {
		for _, spec := range specs {
			if err := validateSpecialization(spec); err != nil {
				return "", agentInstallOptions{}, err
			}
		}
	}

	return app, agentInstallOptions{dir: dir, dirSet: dirCounter.count == 1, agent: agentName, specializations: specs}, nil
}
