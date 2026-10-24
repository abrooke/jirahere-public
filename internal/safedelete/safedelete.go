package safedelete

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aslanbrooke/jirahere/internal/layout"
)

type ErrOutsideBase struct {
	Base   string
	Target string
	Reason string
}

func (e *ErrOutsideBase) Error() string {
	return fmt.Sprintf(
		"refusing to delete target %s outside base %s: %s",
		strconv.Quote(e.Target), strconv.Quote(e.Base), e.Reason,
	)
}

func Remove(base, target string) error {
	resolved, err := check(base, target)
	if err != nil {
		return err
	}

	return os.Remove(resolved)
}

func RemoveAll(base, target string) error {
	resolved, err := check(base, target)
	if err != nil {
		return err
	}

	return os.RemoveAll(resolved)
}

func check(base, target string) (string, error) {
	reject := func(reason string) (string, error) {
		return "", &ErrOutsideBase{Base: base, Target: target, Reason: reason}
	}

	if base == "" {
		return reject("base is empty")
	}
	if target == "" {
		return reject("target is empty")
	}

	absBase, err := filepath.Abs(base)
	if err != nil {
		return reject("base is not a resolvable path")
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return reject("target is not a resolvable path")
	}

	absBase = filepath.Clean(absBase)
	absTarget = filepath.Clean(absTarget)

	if hasDotDot(absBase) || hasDotDot(absTarget) {
		return reject("path contains a .. element after cleaning")
	}

	resolvedBase, err := filepath.EvalSymlinks(absBase)
	if err != nil {
		return reject("base does not resolve: " + sanitizeErr(err))
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(absTarget))
	if err != nil {
		return reject("target parent does not resolve: " + sanitizeErr(err))
	}
	resolvedTarget := filepath.Join(resolvedParent, filepath.Base(absTarget))

	home := comparableHome()
	for _, p := range [2]string{resolvedBase, resolvedTarget} {
		if filepath.Dir(p) == p {
			return reject("path is the filesystem root")
		}
		if home != "" && p == home {
			return reject("path is the user's home directory")
		}
	}

	if resolvedTarget == resolvedBase {
		return reject("target is the base directory itself")
	}
	if !strings.HasPrefix(resolvedTarget, resolvedBase+string(os.PathSeparator)) {
		return reject("target is not inside the base directory")
	}

	if !strings.Contains(resolvedTarget, layout.Namespace) {
		return reject("resolved target does not contain the " + strconv.Quote(layout.Namespace) + " namespace token")
	}

	return resolvedTarget, nil
}

func hasDotDot(p string) bool {
	for _, part := range strings.Split(p, string(os.PathSeparator)) {
		if part == ".." {
			return true
		}
	}
	return false
}

func comparableHome() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return ""
	}
	return resolved
}

func sanitizeErr(err error) string {
	return strconv.Quote(err.Error())
}
