package service

import (
	"errors"
	"go-fileserver/internal/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// symlinkOrSkip creates a symlink or skips the test when the platform does not
// allow it (for example Windows without the create-symlink privilege).
func symlinkOrSkip(t *testing.T, oldname, newname string) {
	t.Helper()

	if err := os.Symlink(oldname, newname); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func readString(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// A. Middle symlink component pointing outside the root must not be followed.
func TestResolveExistingRejectsMiddleSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	outsideFile := filepath.Join(outside, "file.txt")
	writeFile(t, outsideFile, "secret")

	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, outside, filepath.Join(root, "a", "link"))

	withSharedRoot(t, root)

	if _, err := ResolveExisting("a/link/file.txt"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("ResolveExisting(middle symlink) err = %v, want ErrAccessDenied", err)
	}
	if _, err := List(model.ListOptions{Path: "a/link"}); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("List(middle symlink) err = %v, want ErrAccessDenied", err)
	}
	if got := readString(t, outsideFile); got != "secret" {
		t.Errorf("outside file changed: %q", got)
	}
}

// B. Upload into a directory reached through an escaping symlink must be
// rejected and must not create the file outside the root.
func TestSaveUploadRejectsSymlinkedParentDir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, outside, filepath.Join(root, "uploads", "link"))

	withSharedRoot(t, root)

	err := SaveUpload("uploads/link", "new.txt", strings.NewReader("data"))
	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("SaveUpload err = %v, want ErrAccessDenied", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("upload escaped the shared root: outside/new.txt exists")
	}
}

// C1. Deleting a symlink to a directory inside the root removes the link only.
func TestDeleteRemovesLinkNotTarget(t *testing.T) {
	root := t.TempDir()

	realDir := filepath.Join(root, "realdir")
	realFile := filepath.Join(realDir, "file.txt")
	writeFile(t, realFile, "keep")

	symlinkOrSkip(t, realDir, filepath.Join(root, "linkdir"))

	withSharedRoot(t, root)

	if err := Delete("linkdir"); err != nil {
		t.Fatalf("Delete(linkdir): %v", err)
	}

	if _, err := os.Stat(realDir); err != nil {
		t.Errorf("target directory was removed: %v", err)
	}
	if got := readString(t, realFile); got != "keep" {
		t.Errorf("target file content changed: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(root, "linkdir")); !os.IsNotExist(err) {
		t.Errorf("link was not removed (err = %v)", err)
	}
}

// C2. Deleting a symlink that escapes the root is rejected and the outside
// target is left untouched.
func TestDeleteRejectsEscapingLink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	victim := filepath.Join(outside, "victim.txt")
	writeFile(t, victim, "victim")

	link := filepath.Join(root, "escape")
	symlinkOrSkip(t, victim, link)

	withSharedRoot(t, root)

	if err := Delete("escape"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("Delete(escape) err = %v, want ErrAccessDenied", err)
	}
	if got := readString(t, victim); got != "victim" {
		t.Errorf("outside victim changed: %q", got)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("escaping link should be preserved on rejection: %v", err)
	}
}

// C3. Deleting through a middle symlink that escapes the root is rejected.
func TestDeleteRejectsMiddleSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	victim := filepath.Join(outside, "victim.txt")
	writeFile(t, victim, "victim")

	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, outside, filepath.Join(root, "a", "link"))

	withSharedRoot(t, root)

	if err := Delete("a/link/victim.txt"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("Delete(middle symlink) err = %v, want ErrAccessDenied", err)
	}
	if got := readString(t, victim); got != "victim" {
		t.Errorf("outside victim changed: %q", got)
	}
}

// C4. A dangling link (its target does not exist) is removed directly, because
// removing a link never affects its nonexistent target.
func TestDeleteRemovesDanglingLink(t *testing.T) {
	root := t.TempDir()

	link := filepath.Join(root, "dangling")
	symlinkOrSkip(t, filepath.Join(root, "does-not-exist"), link)

	withSharedRoot(t, root)

	if err := Delete("dangling"); err != nil {
		t.Fatalf("Delete(dangling): %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("dangling link was not removed (err = %v)", err)
	}
}

// D1. Renaming a source reached through an escaping symlinked directory is
// rejected and nothing is created outside the root.
func TestRenameRejectsSymlinkedParentEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	writeFile(t, filepath.Join(outside, "file.txt"), "outside")

	symlinkOrSkip(t, outside, filepath.Join(root, "destlink"))

	withSharedRoot(t, root)

	err := Rename("destlink/file.txt", "renamed.txt")
	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("Rename(symlinked parent) err = %v, want ErrAccessDenied", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "renamed.txt")); !os.IsNotExist(err) {
		t.Errorf("rename escaped the shared root: outside/renamed.txt exists")
	}
	if got := readString(t, filepath.Join(outside, "file.txt")); got != "outside" {
		t.Errorf("outside source changed: %q", got)
	}
}

// D2. A directory-qualified new name cannot redirect the destination through a
// symlink: only the final name component is honoured, so the file stays in its
// original directory.
func TestRenameNewNameCannotRedirectThroughSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	writeFile(t, filepath.Join(root, "sub", "a.txt"), "a")
	symlinkOrSkip(t, outside, filepath.Join(root, "destlink"))

	withSharedRoot(t, root)

	if err := Rename("sub/a.txt", "destlink/b.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outside, "b.txt")); !os.IsNotExist(err) {
		t.Errorf("rename escaped the shared root: outside/b.txt exists")
	}
	if got := readString(t, filepath.Join(root, "sub", "b.txt")); got != "a" {
		t.Errorf("file was not renamed inside its own directory: %q", got)
	}
}

// E. CleanRel rejects Windows drive-relative and UNC-style roots.
func TestCleanRelRejectsRootDesignators(t *testing.T) {
	cases := []string{
		"C:foo",
		`\absolute\path`,
		"//server/share",
		`\\server\share`,
		"C:\\Windows",
		"C:/Windows",
	}

	for _, in := range cases {
		if _, err := CleanRel(in); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("CleanRel(%q) err = %v, want ErrInvalidPath", in, err)
		}
	}
}

// H. Search is a filename substring match and must never be treated as a path.
func TestSearchIsFilenameSubstringNotPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "etc.txt"), "x")

	withSharedRoot(t, root)

	items, err := List(model.ListOptions{Path: "", Search: "../../etc"})
	if err != nil {
		t.Fatalf("List(search=../../etc): %v", err)
	}
	if len(items) != 0 {
		t.Errorf("search returned %d items, want 0", len(items))
	}

	items, err = List(model.ListOptions{Path: "", Search: "etc"})
	if err != nil {
		t.Fatalf("List(search=etc): %v", err)
	}
	if len(items) != 1 || items[0].Name != "etc.txt" {
		t.Errorf("substring search broken: got %d items", len(items))
	}
}

// I. Rename same name is rejected as a conflict.
func TestRenameSameNameIsConflict(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "a")

	withSharedRoot(t, root)

	if err := Rename("a.txt", "a.txt"); !errors.Is(err, ErrFileExists) {
		t.Fatalf("Rename same name err = %v, want ErrFileExists", err)
	}
}

// I. Renaming a directory keeps it inside the root.
func TestRenameDirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "olddir", "file.txt"), "x")

	withSharedRoot(t, root)

	if err := Rename("olddir", "newdir"); err != nil {
		t.Fatalf("Rename directory: %v", err)
	}
	if got := readString(t, filepath.Join(root, "newdir", "file.txt")); got != "x" {
		t.Errorf("renamed directory content = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "olddir")); !os.IsNotExist(err) {
		t.Errorf("old directory still exists")
	}
}
