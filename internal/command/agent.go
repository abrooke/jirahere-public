package command

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrAgentHomeUnavailable = errors.New("could not resolve home directory")

var ErrAgentTargetUnsafe = errors.New("agent target contains unsafe control characters")

var ErrAgentSpecializationStem = errors.New("agent specialization stem is invalid")

const maxAgentFilenameBytes = 255

var agentInstallFilenames = map[string]map[string]string{
	"jirasistant": {
		"claude": "jirasistant.md",
		"codex":  "jirasistant.config.toml",
	},
	"jirasistant-req": {
		"claude": "jirasistant-req.md",
		"codex":  "jirasistant-req.config.toml",
	},
}

func ResolveAgentInstallTarget(agent, app string, targetDir *string, stems ...string) (string, error) {
	apps, ok := agentInstallFilenames[agent]
	if !ok {
		return "", fmt.Errorf("unsupported agent %q", agent)
	}
	filename, ok := apps[app]
	if !ok {
		return "", fmt.Errorf("unsupported agent app %q", app)
	}

	if len(stems) > 0 {
		ext := ".md"
		if app == "codex" {
			ext = ".config.toml"
		}
		combined, err := combinedAgentFilename(agent, stems, ext)
		if err != nil {
			return "", err
		}
		filename = combined
	}

	var root string
	switch {
	case targetDir != nil:
		root = *targetDir
	case app == "claude":
		configDir, set := os.LookupEnv("CLAUDE_CONFIG_DIR")
		if !set {
			configDir = "~/.claude"
		}
		root = filepath.Join(configDir, "agents")
	default:
		codexHome, set := os.LookupEnv("CODEX_HOME")
		if !set {
			codexHome = "~/.codex"
		}
		root = codexHome
	}

	root, err := expandAndAbsSkillsPath(root)
	if err != nil {
		switch {
		case errors.Is(err, ErrSkillsHomeUnavailable):
			return "", ErrAgentHomeUnavailable
		case errors.Is(err, ErrSkillsTargetUnsafe):
			return "", ErrAgentTargetUnsafe
		}
		return "", err
	}
	return filepath.Join(root, filename), nil
}

func combinedAgentFilename(agent string, stems []string, ext string) (string, error) {
	name := agent
	for _, stem := range stems {
		if stem == "" {
			return "", ErrAgentSpecializationStem
		}
		for i := 0; i < len(stem); i++ {
			c := stem[i]
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '.' && c != '_' && c != '-' {
				return "", ErrAgentSpecializationStem
			}
		}
		name += "-" + stem
	}
	name += ext
	if len(name) > maxAgentFilenameBytes {
		return "", ErrAgentSpecializationStem
	}
	return name, nil
}
