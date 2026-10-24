package auth

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

var readPassword = term.ReadPassword

func IsTerminal(fd uintptr) bool {
	return term.IsTerminal(int(fd))
}

func ReadAPIToken(stdinFd uintptr, stdin io.Reader) (string, error) {
	return readAPIToken(IsTerminal(stdinFd), stdinFd, stdin)
}

func readAPIToken(isTerminal bool, stdinFd uintptr, stdin io.Reader) (string, error) {
	if isTerminal {
		return readSecretInteractive(stdinFd)
	}
	return readSecretLine(stdin)
}

func readSecretInteractive(fd uintptr) (string, error) {
	fmt.Fprint(os.Stderr, "API token: ")
	b, err := readPassword(int(fd))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("could not read API token: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

func readSecretLine(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("could not read API token: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
