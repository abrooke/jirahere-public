package command

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failReader struct{ called bool }

func (f *failReader) Read([]byte) (int, error) { f.called = true; return 0, errors.New("boom") }

func writeBody(t *testing.T, content []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadBodyFile_Path(t *testing.T) {
	p := writeBody(t, []byte("it's a body\nline two"))
	got, err := ReadBodyFile(p, &failReader{})
	if err != nil || got != "it's a body\nline two" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestReadBodyFile_PathDoesNotTouchStdin(t *testing.T) {
	fr := &failReader{}
	if _, err := ReadBodyFile(writeBody(t, []byte("x")), fr); err != nil {
		t.Fatal(err)
	}
	if fr.called {
		t.Fatal("stdin was read for a file path")
	}
}

func TestReadBodyFile_Stdin(t *testing.T) {
	got, err := ReadBodyFile("-", strings.NewReader("from stdin\n"))
	if err != nil || got != "from stdin" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestReadBodyFile_TrimsTrailingNewlines(t *testing.T) {
	for in, want := range map[string]string{
		"a\n":         "a",
		"a\r\n":       "a",
		"a\r\n\r\n\n": "a",
		"a\n b \n":    "a\n b ",
		"a  \n":       "a  ",
		"\n\na\n":     "\n\na",
	} {
		got, err := ReadBodyFile(writeBody(t, []byte(in)), nil)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestReadBodyFile_Errors(t *testing.T) {
	secret := "SECRET-TOKEN-123"
	cases := []struct {
		name string
		path string
		in   []byte
		want error
	}{
		{"unreadable", filepath.Join(t.TempDir(), "missing"), nil, ErrBodyFileUnreadable},
		{"directory", t.TempDir(), nil, ErrBodyFileUnreadable},
		{"empty", "", []byte(""), ErrBodyFileEmpty},
		{"whitespace", "", []byte(" \t\r\n\n"), ErrBodyFileEmpty},
		{"invalid utf8", "", []byte(secret + "\xff\xfe"), ErrBodyFileNotUTF8},
		{"too large", "", []byte(secret + strings.Repeat("a", MaxBodyFileBytes)), ErrBodyFileTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := c.path
			if path == "" {
				path = writeBody(t, c.in)
			}
			_, err := ReadBodyFile(path, nil)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want kind %v", err, c.want)
			}
			var be *BodyFileError
			if !errors.As(err, &be) {
				t.Fatalf("not a *BodyFileError: %T", err)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error %q does not name path", err)
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error echoes content: %q", err)
			}
			for _, other := range []error{ErrBodyFileUnreadable, ErrBodyFileEmpty, ErrBodyFileNotUTF8, ErrBodyFileTooLarge} {
				if other != c.want && errors.Is(err, other) {
					t.Errorf("error also matches %v", other)
				}
			}
		})
	}
}

func TestReadBodyFile_StdinErrors(t *testing.T) {
	_, err := ReadBodyFile("-", strings.NewReader("  \n"))
	if !errors.Is(err, ErrBodyFileEmpty) || !strings.Contains(err.Error(), "stdin") {
		t.Errorf("empty stdin: %v", err)
	}
	if _, err := ReadBodyFile("-", &failReader{}); !errors.Is(err, ErrBodyFileUnreadable) {
		t.Errorf("failing stdin: %v", err)
	}
	if _, err := ReadBodyFile("-", nil); !errors.Is(err, ErrBodyFileUnreadable) {
		t.Errorf("nil stdin: %v", err)
	}
}

func TestReadBodyFile_BOM(t *testing.T) {
	const bom = "\xef\xbb\xbf"
	for name, c := range map[string]struct{ in, want string }{
		"strip":        {bom + "hello\n", "hello"},
		"mid-text":     {"a" + bom + "b\n", "a" + bom + "b"},
		"double":       {bom + bom + "x", bom + "x"},
		"bom then ws":  {bom + "\n x", "\n x"},
		"trailing bom": {"x" + bom, "x" + bom},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ReadBodyFile(writeBody(t, []byte(c.in)), nil)
			if err != nil || got != c.want {
				t.Errorf("file: got %q, %v; want %q", got, err, c.want)
			}
			got, err = ReadBodyFile("-", strings.NewReader(c.in))
			if err != nil || got != c.want {
				t.Errorf("stdin: got %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

func TestReadBodyFile_BOMOnlyIsEmpty(t *testing.T) {
	const bom = "\xef\xbb\xbf"
	for _, in := range []string{bom, bom + "\n", bom + " \t\r\n"} {
		if _, err := ReadBodyFile(writeBody(t, []byte(in)), nil); !errors.Is(err, ErrBodyFileEmpty) {
			t.Errorf("file %q: %v", in, err)
		}
		if _, err := ReadBodyFile("-", strings.NewReader(in)); !errors.Is(err, ErrBodyFileEmpty) {
			t.Errorf("stdin %q: %v", in, err)
		}
	}
}

func TestReadBodyFile_Symlink(t *testing.T) {
	target := writeBody(t, []byte("via link\n"))
	link := filepath.Join(t.TempDir(), "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	got, err := ReadBodyFile(link, nil)
	if err != nil || got != "via link" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestReadBodyFile_StdinErrorsDoNotEchoContent(t *testing.T) {
	secret := "SECRET-TOKEN-123"
	for name, in := range map[string]string{
		"invalid utf8": secret + "\xff\xfe",
		"too large":    secret + strings.Repeat("a", MaxBodyFileBytes),
	} {
		_, err := ReadBodyFile("-", strings.NewReader(in))
		if err == nil || strings.Contains(err.Error(), secret) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestReadBodyFile_SizeBoundary(t *testing.T) {
	exact := strings.Repeat("a", MaxBodyFileBytes)
	got, err := ReadBodyFile("-", strings.NewReader(exact))
	if err != nil || len(got) != MaxBodyFileBytes {
		t.Fatalf("exactly 1 MiB: len=%d err=%v", len(got), err)
	}
	if _, err := ReadBodyFile("-", strings.NewReader(exact+"a")); !errors.Is(err, ErrBodyFileTooLarge) {
		t.Fatalf("1 MiB + 1: %v", err)
	}

	if _, err := ReadBodyFile("-", strings.NewReader(exact+"\n")); !errors.Is(err, ErrBodyFileTooLarge) {
		t.Fatalf("1 MiB + newline: %v", err)
	}
}
