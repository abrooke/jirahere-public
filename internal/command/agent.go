package command

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrAgentHomeUnavailable = errors.New("could not resolve home directory")

var ErrAgentTargetUnsafe = errors.New("agent target contains unsafe control characters")

const (
	claudeAgentFilename = "jirasistant.md"
	codexAgentFilename  = "jirasistant.config.toml"
)

func ResolveAgentInstallTarget(app string, targetDir *string) (string, error) {
	var filename string
	switch app {
	case "claude":
		filename = claudeAgentFilename
	case "codex":
		filename = codexAgentFilename
	default:
		return "", fmt.Errorf("unsupported agent app %q", app)
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
