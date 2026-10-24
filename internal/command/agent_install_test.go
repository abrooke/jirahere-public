package command

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func assertNoAgentTemp(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "jirasistant.install-") {
			t.Errorf("leftover temp file %q in %s", e.Name(), dir)
		}
	}
}

func TestInstallAgentFile_WritesThenReplaces(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "jirasistant.md")

	replaced, err := InstallAgentFile(target, []byte("one"))
	if err != nil || replaced {
		t.Fatalf("first install: replaced=%v err=%v, want written", replaced, err)
	}
	replaced, err = InstallAgentFile(target, []byte("two"))
	if err != nil || !replaced {
		t.Fatalf("second install: replaced=%v err=%v, want replaced", replaced, err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "two" {
		t.Errorf("target = %q, %v, want %q", got, err, "two")
	}
	assertNoAgentTemp(t, dir)
}

func TestInstallAgentFile_NormalizesModeTo0644(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "jirasistant.md")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	if _, err := InstallAgentFile(target, []byte("new")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", info.Mode().Perm())
	}
}

func TestInstallAgentFile_CreatesMissingParentsMode0755AndKeepsExistingParent(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	a := filepath.Join(root, "a")
	b := filepath.Join(a, "b")
	target := filepath.Join(b, "jirasistant.md")

	replaced, err := InstallAgentFile(target, []byte("x"))
	if err != nil || replaced {
		t.Fatalf("replaced=%v err=%v, want written", replaced, err)
	}
	for _, d := range []string{a, b} {
		info, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Errorf("%s mode = %v, want 0755", d, info.Mode().Perm())
		}
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("pre-existing dir mode = %v, want untouched 0700", info.Mode().Perm())
	}
}

func TestInstallAgentFile_ParentUncreatableFailsWithoutWriting(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := InstallAgentFile(filepath.Join(blocker, "sub", "jirasistant.md"), []byte("x"))
	if !errors.Is(err, ErrAgentPrepareTarget) {
		t.Fatalf("err = %v, want ErrAgentPrepareTarget", err)
	}
	if got, _ := os.ReadFile(blocker); string(got) != "file" {
		t.Errorf("blocker changed to %q", got)
	}
	assertNoAgentTemp(t, root)
}

func TestInstallAgentFile_RenameFailureLeavesTargetUnchangedAndRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "jirasistant.md")

	if err := os.MkdirAll(filepath.Join(target, "keep"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := InstallAgentFile(target, []byte("x"))
	if !errors.Is(err, ErrAgentWrite) {
		t.Fatalf("err = %v, want ErrAgentWrite", err)
	}
	if info, statErr := os.Stat(filepath.Join(target, "keep")); statErr != nil || !info.IsDir() {
		t.Errorf("target directory changed: %v", statErr)
	}
	assertNoAgentTemp(t, dir)
}

func TestInstallAgentFile_TempCreationFailureLeavesTargetUnchanged(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not bind root")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "jirasistant.md")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := InstallAgentFile(target, []byte("new"))
	if !errors.Is(err, ErrAgentWrite) {
		t.Fatalf("err = %v, want ErrAgentWrite", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "original" {
		t.Errorf("target = %q, want unchanged", got)
	}
	assertNoAgentTemp(t, dir)
}

func TestInstallAgentFile_ReplacesSymlinkWithoutFollowingIt(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "jirasistant.md")
	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}

	replaced, err := InstallAgentFile(target, []byte("new"))
	if err != nil || !replaced {
		t.Fatalf("replaced=%v err=%v", replaced, err)
	}
	if got, _ := os.ReadFile(outside); string(got) != "precious" {
		t.Errorf("symlink destination was written through: %q", got)
	}
	info, err := os.Lstat(target)
	if err != nil || !info.Mode().IsRegular() {
		t.Errorf("target = %v, %v, want regular file", info, err)
	}
}
