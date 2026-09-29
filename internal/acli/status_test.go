package acli

import (
	"context"
	"errors"
	"testing"
)

func TestLoggedIn_Authenticated(t *testing.T) {
	orig := runCommand
	defer func() { runCommand = orig }()
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
		return []byte("Logged in as someone@example.com\n"), nil, 0, nil
	}

	ok, err := LoggedIn(context.Background())
	if err != nil {
		t.Fatalf("LoggedIn() error = %v, want nil", err)
	}
	if !ok {
		t.Error("LoggedIn() = false, want true when acli exits 0")
	}
}

func TestLoggedIn_NotAuthenticated(t *testing.T) {
	orig := runCommand
	defer func() { runCommand = orig }()
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
		return nil, []byte("✗ Error: unauthorized: use 'acli jira auth login' to authenticate\n"), 1, nil
	}

	ok, err := LoggedIn(context.Background())
	if err != nil {
		t.Fatalf("LoggedIn() error = %v, want nil for a confirmed not-authenticated result", err)
	}
	if ok {
		t.Error("LoggedIn() = true, want false when acli reports unauthorized")
	}
}

func TestLoggedIn_ExecutionFailure(t *testing.T) {
	orig := runCommand
	defer func() { runCommand = orig }()
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
		return nil, nil, -1, errors.New(`exec: "acli": executable file not found in $PATH`)
	}

	ok, err := LoggedIn(context.Background())
	if err == nil {
		t.Fatal("LoggedIn() error = nil, want non-nil when acli can't be run")
	}
	if ok {
		t.Error("LoggedIn() = true, want false when acli can't be run")
	}
}

func TestLoggedIn_UnrecognizedOutput(t *testing.T) {
	orig := runCommand
	defer func() { runCommand = orig }()
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
		return nil, []byte("✗ Error: unknown flag: --bogus\n"), 1, nil
	}

	ok, err := LoggedIn(context.Background())
	if err == nil {
		t.Fatal("LoggedIn() error = nil, want non-nil for an unrecognized non-zero exit")
	}
	if ok {
		t.Error("LoggedIn() = true, want false for an unrecognized non-zero exit")
	}
}
