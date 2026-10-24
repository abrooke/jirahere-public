package command

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const AgentInstallTempPattern = "jirasistant.install-*"

var ErrAgentPrepareTarget = errors.New("could not prepare agent target directory")

var ErrAgentWrite = errors.New("could not write agent file")

func InstallAgentFile(target string, contents []byte) (replaced bool, err error) {
	dir := filepath.Dir(target)
	if err := ensureAgentParent(dir); err != nil {
		return false, fmt.Errorf("%w: %w", ErrAgentPrepareTarget, err)
	}

	if _, err := os.Lstat(target); err == nil {
		replaced = true
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("%w: %w", ErrAgentWrite, err)
	}

	if err := writeAgentFile(dir, target, contents); err != nil {
		return false, fmt.Errorf("%w: %w", ErrAgentWrite, err)
	}
	return replaced, nil
}

func writeAgentFile(dir, target string, contents []byte) (err error) {
	temp, err := os.CreateTemp(dir, AgentInstallTempPattern)
	if err != nil {
		return err
	}
	name := temp.Name()
	defer func() {
		if err != nil {
			_ = temp.Close()
			_ = os.Remove(name)
		}
	}()

	if _, err = temp.Write(contents); err != nil {
		return err
	}
	if err = temp.Chmod(0o644); err != nil {
		return err
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, target)
}

func ensureAgentParent(dir string) error {
	var missing []string
	for current := filepath.Clean(dir); ; {
		_, err := os.Lstat(current)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return err
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, created := range missing {
		if err := os.Chmod(created, 0o755); err != nil {
			return err
		}
	}
	return nil
}
