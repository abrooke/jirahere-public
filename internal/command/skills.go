package command

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

var ErrSkillsHomeUnavailable = errors.New("could not resolve home directory")

var ErrSkillsTargetUnsafe = errors.New("skills target contains unsafe control characters")

func ResolveSkillsInstallTarget(app string, targetDir *string) (string, error) {
	if targetDir == nil {
		var defaultTargetDir string
		switch app {
		case "claude":
			var set bool
			defaultTargetDir, set = os.LookupEnv("CLAUDE_CONFIG_DIR")
			if !set {
				defaultTargetDir = "~/.claude"
			}
		case "codex":
			var set bool
			defaultTargetDir, set = os.LookupEnv("CODEX_HOME")
			if !set {
				defaultTargetDir = "~/.codex"
			}
		default:
			return "", fmt.Errorf("unsupported skills app %q", app)
		}
		return expandAndAbsSkillsPath(filepath.Join(defaultTargetDir, "skills"))
	}

	return expandAndAbsSkillsPath(*targetDir)
}

func expandAndAbsSkillsPath(path string) (string, error) {
	path = os.ExpandEnv(path)
	if path == "~" || strings.HasPrefix(path, "~/") {
		home := os.Getenv("HOME")
		if home == "" {
			return "", ErrSkillsHomeUnavailable
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if strings.IndexFunc(path, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	}) >= 0 {
		return "", ErrSkillsTargetUnsafe
	}
	return path, nil
}
