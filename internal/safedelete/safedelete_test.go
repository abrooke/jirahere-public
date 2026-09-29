package safedelete_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/layout"
	"github.com/aslanbrooke/jirahere/internal/quarter"
	"github.com/aslanbrooke/jirahere/internal/safedelete"
)

func mustMkdir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", dir, err)
	}
	return dir
}

func mustWrite(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
	return path
}

func rejection(t *testing.T, err error) *safedelete.ErrOutsideBase {
	t.Helper()
	if err == nil {
		t.Fatal("expected a rejection, got nil error")
	}
	var e *safedelete.ErrOutsideBase
	if !errors.As(err, &e) {
		t.Fatalf("expected *safedelete.ErrOutsideBase, got %T: %v", err, err)
	}
	return e
}

func assertRejected(t *testing.T, err error) {
	t.Helper()
	_ = rejection(t, err)
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("expected %s to still exist after a rejected delete: %v", path, err)
	}
}

func assertGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be gone, Lstat err = %v", path, err)
	}
}

func TestRemoveAll_InBoundsJirahereTarget(t *testing.T) {
	base := mustMkdir(t, filepath.Join(t.TempDir(), "jirahere-base"))
	target := mustMkdir(t, filepath.Join(base, "child"))
	mustWrite(t, filepath.Join(target, "f"))

	if err := safedelete.RemoveAll(base, target); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	assertGone(t, target)
	assertExists(t, base)
}

func TestRemove_InBoundsJirahereTarget(t *testing.T) {
	base := mustMkdir(t, filepath.Join(t.TempDir(), "jirahere-base"))
	target := mustWrite(t, filepath.Join(base, "f"))

	if err := safedelete.Remove(base, target); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	assertGone(t, target)
}

func TestRemoveAll_DirectoryItself(t *testing.T) {
	dir := mustMkdir(t, filepath.Join(t.TempDir(), "jirahere-cache"))
	mustWrite(t, filepath.Join(dir, "inventory.json"))

	if err := safedelete.RemoveAll(filepath.Dir(dir), dir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	assertGone(t, dir)
}

func TestRemoveAll_NonExistentTargetIsNotAnError(t *testing.T) {
	base := mustMkdir(t, filepath.Join(t.TempDir(), "jirahere-base"))
	if err := safedelete.RemoveAll(base, filepath.Join(base, "absent")); err != nil {
		t.Fatalf("RemoveAll of an absent target: %v", err)
	}
}

func TestRemove_NonExistentTargetIsNotExist(t *testing.T) {
	base := mustMkdir(t, filepath.Join(t.TempDir(), "jirahere-base"))
	err := safedelete.Remove(base, filepath.Join(base, "absent"))
	if !os.IsNotExist(err) {
		t.Fatalf("Remove of an absent target: err = %v, want an os.IsNotExist error", err)
	}
}

func TestRemoveAll_AbsentParentIsRejected(t *testing.T) {
	base := mustMkdir(t, filepath.Join(t.TempDir(), "jirahere-base"))

	target := filepath.Join(base, "never-made", "child")

	e := rejection(t, safedelete.RemoveAll(base, target))
	if !strings.Contains(e.Reason, "parent") {
		t.Errorf("Reason = %q, want it to mention the unresolvable parent", e.Reason)
	}
	assertExists(t, base)
}

func TestRejects_DotDotEscape(t *testing.T) {
	base := mustMkdir(t, filepath.Join(t.TempDir(), "jirahere-base"))
	sibling := mustWrite(t, filepath.Join(filepath.Dir(base), "jirahere-sibling"))

	err := safedelete.RemoveAll(base, filepath.Join(base, "..", "jirahere-sibling"))
	assertRejected(t, err)
	assertExists(t, sibling)
}

func TestRejects_AbsolutePathEscape(t *testing.T) {
	base := mustMkdir(t, filepath.Join(t.TempDir(), "jirahere-base"))
	outside := mustWrite(t, filepath.Join(t.TempDir(), "jirahere-outside", "x"))

	err := safedelete.RemoveAll(base, outside)
	assertRejected(t, err)
	assertExists(t, outside)
}

func TestRejects_SymlinkedParentEscape(t *testing.T) {
	root := t.TempDir()
	base := mustMkdir(t, filepath.Join(root, "jirahere-base"))
	realOutside := mustMkdir(t, filepath.Join(root, "jirahere-outside"))
	child := mustWrite(t, filepath.Join(realOutside, "child"))

	if err := os.Symlink(realOutside, filepath.Join(base, "link")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	err := safedelete.RemoveAll(base, filepath.Join(base, "link", "child"))
	assertRejected(t, err)
	assertExists(t, child)
}

func TestRemoveAll_FinalElementSymlinkIsNotFollowed(t *testing.T) {
	root := t.TempDir()
	base := mustMkdir(t, filepath.Join(root, "jirahere-base"))
	precious := mustMkdir(t, filepath.Join(root, "jirahere-precious"))
	kept := mustWrite(t, filepath.Join(precious, "keep"))

	link := filepath.Join(base, "jirahere-link")
	if err := os.Symlink(precious, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if err := safedelete.RemoveAll(base, link); err != nil {
		t.Fatalf("RemoveAll of a jirahere-named symlink under base: %v", err)
	}
	assertGone(t, link)
	assertExists(t, precious)
	assertExists(t, kept)
}

func TestRejects_TargetEqualsBase(t *testing.T) {
	base := mustMkdir(t, filepath.Join(t.TempDir(), "jirahere-base"))

	assertRejected(t, safedelete.RemoveAll(base, base))
	assertRejected(t, safedelete.RemoveAll(base, base+string(os.PathSeparator)+"."))
	assertExists(t, base)
}

func TestRejects_FilesystemRoot(t *testing.T) {
	root := string(os.PathSeparator)

	assertRejected(t, safedelete.RemoveAll(root, filepath.Join(root, "jirahere-x")))
	assertRejected(t, safedelete.RemoveAll(filepath.Join(root, "etc"), root))
}

func TestRejects_HomeDirectory(t *testing.T) {
	home := mustMkdir(t, filepath.Join(t.TempDir(), "myhome"))
	t.Setenv("HOME", home)

	err := safedelete.RemoveAll(filepath.Dir(home), home)
	e := rejection(t, err)
	if !strings.Contains(e.Reason, "home") {
		t.Errorf("Reason = %q, want it to mention the home directory", e.Reason)
	}
	assertExists(t, home)
}

func TestHomeUnset_TreatedAsNoHome(t *testing.T) {
	t.Setenv("HOME", "")

	base := mustMkdir(t, filepath.Join(t.TempDir(), "jirahere-base"))

	e := rejection(t, safedelete.RemoveAll(base, ""))
	if !strings.Contains(e.Reason, "target is empty") {
		t.Errorf("Reason = %q, want %q", e.Reason, "target is empty")
	}

	target := mustMkdir(t, filepath.Join(base, "child"))
	if err := safedelete.RemoveAll(base, target); err != nil {
		t.Fatalf("RemoveAll with $HOME unset: %v", err)
	}
	assertGone(t, target)
}

func TestRejects_StrictDescendantWithoutJirahereToken(t *testing.T) {
	base := t.TempDir()
	if strings.Contains(base, layout.Namespace) {
		t.Skipf("temp dir %q unexpectedly contains %q", base, layout.Namespace)
	}
	child := mustMkdir(t, filepath.Join(base, "child"))

	err := safedelete.RemoveAll(base, child)
	e := rejection(t, err)
	if !strings.Contains(e.Reason, layout.Namespace) {
		t.Errorf("Reason = %q, want it to mention the %q token", e.Reason, layout.Namespace)
	}
	assertExists(t, child)
}

func TestSubstringNotSegment(t *testing.T) {
	base := mustMkdir(t, filepath.Join(t.TempDir(), "skills-root", "jirahere-foo"))
	if strings.Contains(filepath.Dir(base), layout.Namespace) {
		t.Skipf("temp dir %q unexpectedly contains %q", filepath.Dir(base), layout.Namespace)
	}
	target := mustMkdir(t, filepath.Join(base, "child"))

	if err := safedelete.RemoveAll(base, target); err != nil {
		t.Fatalf("RemoveAll of a jirahere-foo descendant: %v", err)
	}
	assertGone(t, target)
}

func TestRejects_EmptyInputs(t *testing.T) {
	base := mustMkdir(t, filepath.Join(t.TempDir(), "jirahere-base"))

	for _, tc := range []struct {
		name         string
		base, target string
		wantReason   string
	}{
		{"empty base", "", "x", "base is empty"},
		{"empty target", base, "", "target is empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := rejection(t, safedelete.RemoveAll(tc.base, tc.target))
			if e.Reason != tc.wantReason {
				t.Errorf("Reason = %q, want %q", e.Reason, tc.wantReason)
			}
			e = rejection(t, safedelete.Remove(tc.base, tc.target))
			if e.Reason != tc.wantReason {
				t.Errorf("Remove Reason = %q, want %q", e.Reason, tc.wantReason)
			}
		})
	}
}

func TestErrOutsideBase_SingleLineRendering(t *testing.T) {
	err := safedelete.RemoveAll("/no/such\nbase", "/no/such\ntarget")
	e := rejection(t, err)
	if strings.ContainsAny(e.Error(), "\n\r\t") {
		t.Errorf("Error() contains a raw control character: %q", e.Error())
	}
	if !strings.Contains(e.Error(), `\n`) {
		t.Errorf("Error() = %q, want the newline escaped, not stripped", e.Error())
	}
}

func TestSharedNamespaceConstant_MatchesPathBuilders(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	configDir, err := auth.ConfigDir("")
	if err != nil {
		t.Fatalf("auth.ConfigDir: %v", err)
	}
	if !strings.Contains(configDir, layout.Namespace) {
		t.Errorf("auth.ConfigDir() = %q, want it to contain %q", configDir, layout.Namespace)
	}

	cacheDir, err := quarter.CacheDir("")
	if err != nil {
		t.Fatalf("quarter.CacheDir: %v", err)
	}
	if !strings.Contains(cacheDir, layout.Namespace) {
		t.Errorf("quarter.CacheDir() = %q, want it to contain %q", cacheDir, layout.Namespace)
	}

}
