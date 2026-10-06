package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestReadSecretLine(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"trailing newline", "my-secret-token\n", "my-secret-token"},
		{"crlf", "my-secret-token\r\n", "my-secret-token"},
		{"no trailing newline (EOF)", "my-secret-token", "my-secret-token"},
		{"empty", "\n", ""},
		{"only first line used", "first\nsecond\n", "first"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readSecretLine(strings.NewReader(tc.input))
			if err != nil {
				t.Fatalf("readSecretLine: %v", err)
			}
			if got != tc.want {
				t.Errorf("readSecretLine(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestReadAPIToken_NonTerminalUsesStdinReader(t *testing.T) {
	r := strings.NewReader("piped-secret\n")
	got, err := readAPIToken(false, 0, r)
	if err != nil {
		t.Fatalf("readAPIToken: %v", err)
	}
	if got != "piped-secret" {
		t.Errorf("got %q, want %q", got, "piped-secret")
	}
}

func TestReadAPIToken_TerminalUsesPasswordPrompt(t *testing.T) {
	orig := readPassword
	defer func() { readPassword = orig }()

	called := false
	readPassword = func(fd int) ([]byte, error) {
		called = true
		return []byte("typed-secret"), nil
	}

	got, err := readAPIToken(true, 0, strings.NewReader("should not be read"))
	if err != nil {
		t.Fatalf("readAPIToken: %v", err)
	}
	if !called {
		t.Fatal("terminal branch did not call readPassword")
	}
	if got != "typed-secret" {
		t.Errorf("got %q, want %q", got, "typed-secret")
	}
}

func TestReadAPIToken_TerminalPropagatesError(t *testing.T) {
	orig := readPassword
	defer func() { readPassword = orig }()

	wantErr := errors.New("no such device")
	readPassword = func(fd int) ([]byte, error) {
		return nil, wantErr
	}

	_, err := readAPIToken(true, 0, strings.NewReader(""))
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want wrapping %v", err, wantErr)
	}
}
