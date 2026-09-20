package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// invalidEntryNames lists every name shape a creation form must reject. None of
// these may be silently reduced to a different name.
var invalidEntryNames = []struct {
	name  string
	input string
}{
	{"empty", ""},
	{"dot", "."},
	{"dot dot", ".."},
	{"slash", "a/b"},
	{"nested path", "sub/file.txt"},
	{"trailing slash", "folder/"},
	{"backslash", `a\b`},
	{"windows path", `sub\file.txt`},
	{"absolute posix", "/etc/passwd"},
	{"absolute windows", `C:\Windows`},
	{"absolute windows forward", "C:/Windows"},
	{"drive relative", "C:foo"},
	{"unc", `\\server\share`},
	{"unc forward", "//server/share"},
	{"nul byte", "bad\x00name"},
	{"inner parent", "a/../b"},
	{"inner dot", "a/./b"},
}

// validEntryNames lists names that must be accepted, including Unicode and
// characters that are special in HTML or URLs.
var validEntryNames = []struct {
	name  string
	input string
}{
	{"plain", "notes.txt"},
	{"no extension", "README"},
	{"space", "my documents"},
	{"ampersand", "a&b.txt"},
	{"hash", "a#b.txt"},
	{"percent", "a%b.txt"},
	{"plus", "a+b.txt"},
	{"equals", "a=b.txt"},
	{"brackets", "a b&c[1].txt"},
	{"unicode", "résumé-ünïcode.txt"},
	{"cjk", "日本語.txt"},
	{"dots", "archive.tar.gz"},
	{"leading dot", ".env"},
}

func TestCreateDirectoryAcceptsValidNames(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	for _, tc := range validEntryNames {
		t.Run(tc.name, func(t *testing.T) {
			if err := CreateDirectory("", tc.input); err != nil {
				t.Fatalf("CreateDirectory(%q): %v", tc.input, err)
			}

			info, err := os.Stat(filepath.Join(root, tc.input))
			if err != nil {
				t.Fatalf("directory %q was not created: %v", tc.input, err)
			}
			if !info.IsDir() {
				t.Errorf("%q is not a directory", tc.input)
			}
		})
	}
}

func TestCreateEmptyFileAcceptsValidNames(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	for _, tc := range validEntryNames {
		t.Run(tc.name, func(t *testing.T) {
			if err := CreateEmptyFile("", tc.input); err != nil {
				t.Fatalf("CreateEmptyFile(%q): %v", tc.input, err)
			}

			info, err := os.Stat(filepath.Join(root, tc.input))
			if err != nil {
				t.Fatalf("file %q was not created: %v", tc.input, err)
			}
			if info.IsDir() {
				t.Errorf("%q is a directory, want a file", tc.input)
			}
			if info.Size() != 0 {
				t.Errorf("%q size = %d, want 0", tc.input, info.Size())
			}
		})
	}
}

// Creation happens inside the selected directory, not the root.
func TestCreateInsideNestedDirectory(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.MkdirAll(filepath.Join(root, "documents", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := CreateDirectory("documents/projects", "reports"); err != nil {
		t.Fatalf("CreateDirectory: %v", err)
	}
	if err := CreateEmptyFile("documents/projects", "notes.txt"); err != nil {
		t.Fatalf("CreateEmptyFile: %v", err)
	}

	if info, err := os.Stat(filepath.Join(root, "documents", "projects", "reports")); err != nil || !info.IsDir() {
		t.Errorf("nested directory not created (err=%v)", err)
	}
	if info, err := os.Stat(filepath.Join(root, "documents", "projects", "notes.txt")); err != nil || info.Size() != 0 {
		t.Errorf("nested file not created as zero bytes (err=%v)", err)
	}

	// Nothing may have leaked into the root.
	if _, err := os.Stat(filepath.Join(root, "reports")); !os.IsNotExist(err) {
		t.Errorf("directory was created in the root instead of the selected directory")
	}
	if _, err := os.Stat(filepath.Join(root, "notes.txt")); !os.IsNotExist(err) {
		t.Errorf("file was created in the root instead of the selected directory")
	}
}

func TestCreateRejectsInvalidNames(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	for _, tc := range invalidEntryNames {
		t.Run(tc.name, func(t *testing.T) {
			if err := CreateDirectory("", tc.input); !errors.Is(err, ErrInvalidName) {
				t.Errorf("CreateDirectory(%q) err = %v, want ErrInvalidName", tc.input, err)
			}
			if err := CreateEmptyFile("", tc.input); !errors.Is(err, ErrInvalidName) {
				t.Errorf("CreateEmptyFile(%q) err = %v, want ErrInvalidName", tc.input, err)
			}
		})
	}

	// No entry may have been created anywhere under the root.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("invalid names created entries: %v", entries)
	}
}

func TestCreateDirectoryConflict(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "existing"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "existing.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []string{"existing", "existing.txt"}
	for _, name := range cases {
		if err := CreateDirectory("", name); !errors.Is(err, ErrFileExists) {
			t.Errorf("CreateDirectory(%q) err = %v, want ErrFileExists", name, err)
		}
	}

	if got := readString(t, filepath.Join(root, "existing.txt")); got != "keep" {
		t.Errorf("existing file was modified: %q", got)
	}
}

func TestCreateEmptyFileConflict(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.WriteFile(filepath.Join(root, "existing.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "existing"), 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []string{"existing.txt", "existing"}
	for _, name := range cases {
		if err := CreateEmptyFile("", name); !errors.Is(err, ErrFileExists) {
			t.Errorf("CreateEmptyFile(%q) err = %v, want ErrFileExists", name, err)
		}
	}

	if got := readString(t, filepath.Join(root, "existing.txt")); got != "keep" {
		t.Errorf("existing file was overwritten: %q", got)
	}
}

// A creation destination that is itself a symlink must be a conflict, never
// followed and never written through.
func TestCreateDoesNotFollowDestinationSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	withSharedRoot(t, root)

	victim := filepath.Join(outside, "victim.txt")
	writeFile(t, victim, "secret")

	symlinkOrSkip(t, victim, filepath.Join(root, "link.txt"))

	if err := CreateEmptyFile("", "link.txt"); !errors.Is(err, ErrFileExists) {
		t.Fatalf("CreateEmptyFile(symlink) err = %v, want ErrFileExists", err)
	}
	if got := readString(t, victim); got != "secret" {
		t.Errorf("symlink target was written through: %q", got)
	}

	if err := CreateDirectory("", "link.txt"); !errors.Is(err, ErrFileExists) {
		t.Errorf("CreateDirectory(symlink) err = %v, want ErrFileExists", err)
	}
}

// An escaping symlinked parent directory must be rejected before anything is
// created outside the shared root.
func TestCreateRejectsEscapingSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	withSharedRoot(t, root)

	symlinkOrSkip(t, outside, filepath.Join(root, "escape"))

	if err := CreateEmptyFile("escape", "new.txt"); !errors.Is(err, ErrAccessDenied) {
		t.Errorf("CreateEmptyFile(escaping parent) err = %v, want ErrAccessDenied", err)
	}
	if err := CreateDirectory("escape", "newdir"); !errors.Is(err, ErrAccessDenied) {
		t.Errorf("CreateDirectory(escaping parent) err = %v, want ErrAccessDenied", err)
	}

	if _, err := os.Stat(filepath.Join(outside, "new.txt")); !os.IsNotExist(err) {
		t.Errorf("file escaped the shared root: outside/new.txt exists")
	}
	if _, err := os.Stat(filepath.Join(outside, "newdir")); !os.IsNotExist(err) {
		t.Errorf("directory escaped the shared root: outside/newdir exists")
	}
}

// An in-root symlinked parent is followed (the project's policy for intermediate
// links), so the entry is created in the resolved directory inside the root.
func TestCreateThroughInRootSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, real, filepath.Join(root, "alias"))

	if err := CreateEmptyFile("alias", "notes.txt"); err != nil {
		t.Fatalf("CreateEmptyFile(alias): %v", err)
	}
	if info, err := os.Stat(filepath.Join(real, "notes.txt")); err != nil || info.Size() != 0 {
		t.Errorf("file was not created in the resolved in-root directory (err=%v)", err)
	}
}

func TestCreateMissingParentIsNotFound(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := CreateDirectory("missing", "sub"); !errors.Is(err, ErrNotFound) {
		t.Errorf("CreateDirectory(missing parent) err = %v, want ErrNotFound", err)
	}
	if err := CreateEmptyFile("missing", "a.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("CreateEmptyFile(missing parent) err = %v, want ErrNotFound", err)
	}
}

// A current directory that is a regular file is not a directory.
func TestCreateParentNotADirectory(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "file.txt"), "x")

	if err := CreateDirectory("file.txt", "sub"); !errors.Is(err, ErrNotDirectory) {
		t.Errorf("CreateDirectory(parent=file) err = %v, want ErrNotDirectory", err)
	}
	if err := CreateEmptyFile("file.txt", "a.txt"); !errors.Is(err, ErrNotDirectory) {
		t.Errorf("CreateEmptyFile(parent=file) err = %v, want ErrNotDirectory", err)
	}
}

// A traversal in the current directory is rejected by the existing boundary.
func TestCreateRejectsTraversalParent(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := CreateDirectory("../outside", "sub"); !errors.Is(err, ErrAccessDenied) {
		t.Errorf("CreateDirectory(traversal) err = %v, want ErrAccessDenied", err)
	}
	if err := CreateEmptyFile("../outside", "a.txt"); !errors.Is(err, ErrAccessDenied) {
		t.Errorf("CreateEmptyFile(traversal) err = %v, want ErrAccessDenied", err)
	}
}

func TestCreatePermissionDeniedIsAccessDenied(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	lockDir(t, locked, 0o555)
	if !permissionsEnforced(t, locked) {
		t.Skip("filesystem permissions are not enforced for this user")
	}

	if err := CreateDirectory("locked", "sub"); !errors.Is(err, ErrAccessDenied) {
		t.Errorf("CreateDirectory(denied) err = %v, want ErrAccessDenied", err)
	}
	if err := CreateEmptyFile("locked", "a.txt"); !errors.Is(err, ErrAccessDenied) {
		t.Errorf("CreateEmptyFile(denied) err = %v, want ErrAccessDenied", err)
	}
}

// TestCreateEmptyFileIsAtomic proves the exclusive-create semantics: many
// concurrent attempts on the same name must produce exactly one success and only
// conflicts for the rest. A check-then-create implementation would let more than
// one goroutine win.
func TestCreateEmptyFileIsAtomic(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	const workers = 32

	start := make(chan struct{})
	errs := make([]error, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = CreateEmptyFile("", "race.txt")
		}(i)
	}

	close(start)
	wg.Wait()

	successes, conflicts := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrFileExists):
			conflicts++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}

	if successes != 1 {
		t.Errorf("successes = %d, want exactly 1", successes)
	}
	if conflicts != workers-1 {
		t.Errorf("conflicts = %d, want %d", conflicts, workers-1)
	}

	info, err := os.Stat(filepath.Join(root, "race.txt"))
	if err != nil {
		t.Fatalf("race.txt missing: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("race.txt size = %d, want 0", info.Size())
	}
}

// TestCreateDirectoryIsAtomic proves directory creation is exclusive too.
func TestCreateDirectoryIsAtomic(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	const workers = 32

	start := make(chan struct{})
	errs := make([]error, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = CreateDirectory("", "racedir")
		}(i)
	}

	close(start)
	wg.Wait()

	successes, conflicts := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrFileExists):
			conflicts++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}

	if successes != 1 {
		t.Errorf("successes = %d, want exactly 1", successes)
	}
	if conflicts != workers-1 {
		t.Errorf("conflicts = %d, want %d", conflicts, workers-1)
	}
}

// A client path (as browsers send on upload) is still reduced for uploads, while
// the same shape is rejected for explicit creation. This pins the deliberate
// difference between the two name policies.
func TestCleanEntryNameRejectsPathButCleanNameReduces(t *testing.T) {
	if _, err := cleanEntryName(`client\path\a.txt`); !errors.Is(err, ErrInvalidName) {
		t.Errorf("cleanEntryName(path) err = %v, want ErrInvalidName", err)
	}

	reduced, err := CleanName(`client\path\a.txt`)
	if err != nil {
		t.Fatalf("CleanName(path): %v", err)
	}
	if reduced != "a.txt" {
		t.Errorf("CleanName(path) = %q, want a.txt", reduced)
	}
}

// A directory name cannot silently become a different name.
func TestCreateDoesNotSilentlyRename(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := CreateEmptyFile("", "sub/notes.txt"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("err = %v, want ErrInvalidName", err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "notes") {
			t.Errorf("a differently named file was created: %q", e.Name())
		}
	}
}
