package command

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

const MaxBodyFileBytes = 1 << 20

const BodyFileStdin = "-"

var (
	ErrBodyFileUnreadable = errors.New("could not be read")
	ErrBodyFileEmpty      = errors.New("is empty or whitespace-only")
	ErrBodyFileNotUTF8    = errors.New("is not valid UTF-8")
	ErrBodyFileTooLarge   = fmt.Errorf("exceeds the %d MiB limit", MaxBodyFileBytes>>20)
)

type BodyFileError struct {
	Path   string
	Kind   error
	Reason error
}

func (e *BodyFileError) Error() string {
	src := fmt.Sprintf("body file %q", e.Path)
	if e.Path == BodyFileStdin {
		src = "body from stdin"
	}
	if e.Reason != nil {
		return fmt.Sprintf("%s %s: %s", src, e.Kind, e.Reason)
	}
	return fmt.Sprintf("%s %s", src, e.Kind)
}

func (e *BodyFileError) Is(target error) bool { return target == e.Kind }

func (e *BodyFileError) Unwrap() error { return e.Reason }

func ReadBodyFile(path string, stdin io.Reader) (string, error) {
	var r io.Reader
	if path == BodyFileStdin {
		if stdin == nil {
			return "", &BodyFileError{Path: path, Kind: ErrBodyFileUnreadable}
		}
		r = stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return "", unreadable(path, err)
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxBodyFileBytes+1))
	if err != nil {
		return "", unreadable(path, err)
	}
	if len(data) > MaxBodyFileBytes {
		return "", &BodyFileError{Path: path, Kind: ErrBodyFileTooLarge}
	}
	if !utf8.Valid(data) {
		return "", &BodyFileError{Path: path, Kind: ErrBodyFileNotUTF8}
	}
	text := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
	if strings.TrimSpace(text) == "" {
		return "", &BodyFileError{Path: path, Kind: ErrBodyFileEmpty}
	}
	return strings.TrimRight(text, "\r\n"), nil
}

func unreadable(path string, err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return &BodyFileError{Path: path, Kind: ErrBodyFileUnreadable, Reason: err}
}
