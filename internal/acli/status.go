package acli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const unauthorizedMarker = "unauthorized"

type runner func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, exitCode int, err error)

var runCommand runner = execCommand

func execCommand(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr == nil {
		return stdout.Bytes(), stderr.Bytes(), 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return stdout.Bytes(), stderr.Bytes(), exitErr.ExitCode(), nil
	}

	return stdout.Bytes(), stderr.Bytes(), -1, runErr
}

func LoggedIn(ctx context.Context) (bool, error) {
	_, stderr, exitCode, err := runCommand(ctx, "acli", "jira", "auth", "status")
	if err != nil {
		return false, fmt.Errorf("acli: could not run jira auth status: %w", err)
	}

	if exitCode == 0 {
		return true, nil
	}

	if strings.Contains(strings.ToLower(string(stderr)), unauthorizedMarker) {
		return false, nil
	}

	return false, fmt.Errorf("acli: jira auth status exited %d with unrecognized output", exitCode)
}
