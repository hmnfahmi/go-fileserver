package service

import (
	"errors"
	"go-fileserver/internal/config"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// withEditLimit sets config.MaxEditSize for the duration of the test. It is a
// package global, so tests using it must not run in parallel.
func withEditLimit(t *testing.T, n int64) {
	t.Helper()

	original := config.MaxEditSize
	config.MaxEditSize = n
	t.Cleanup(func() { config.MaxEditSize = original })
}

// --- Editable classification ---------------------------------------------

func TestIsEditableNameSupportedExtensions(t *testing.T) {
	names := []string{
		"a.txt", "a.md", "a.markdown", "a.json", "a.yaml", "a.yml", "a.xml",
		"a.csv", "a.log", "a.sql", "a.go", "a.js", "a.ts", "a.jsx", "a.tsx",
		"a.css", "a.html", "a.htm",
	}

	for _, name := range names {
		if !IsEditableName(name) {
			t.Errorf("IsEditableName(%q) = false, want true", name)
		}
	}
}

func TestIsEditableNameIsCaseInsensitive(t *testing.T) {
	for _, name := range []string{"NOTES.TXT", "Readme.MD", "data.JSON", "index.HTML"} {
		if !IsEditableName(name) {
			t.Errorf("IsEditableName(%q) = false, want true", name)
		}
	}
}

func TestIsEditableNameUnsupportedExtensions(t *testing.T) {
	for _, name := range []string{
		"photo.png", "archive.zip", "movie.mp4", "app.exe", "song.mp3",
		"binary.bin", "noextension", "trailing.", "a.txt.bak",
	} {
		if IsEditableName(name) {
			t.Errorf("IsEditableName(%q) = true, want false", name)
		}
	}
}

// A directory named like an editable file must still be classified as editable
// by name; the regular-file check happens when the file is actually opened.
func TestIsEditableNameIgnoresDirectoryness(t *testing.T) {
	if !IsEditableName("notes.txt") {
		t.Fatal("sanity check failed")
	}
}

// --- Read -----------------------------------------------------------------

func TestReadEditableFileReturnsContentAndVersion(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)
	withEditLimit(t, 1024)

	writeFile(t, filepath.Join(root, "notes.txt"), "hello world")

	doc, err := ReadEditableFile("notes.txt")
	if err != nil {
		t.Fatalf("ReadEditableFile: %v", err)
	}

	if doc.Path != "notes.txt" {
		t.Errorf("Path = %q, want notes.txt", doc.Path)
	}
	if doc.Name != "notes.txt" {
		t.Errorf("Name = %q, want notes.txt", doc.Name)
	}
	if doc.Content != "hello world" {
		t.Errorf("Content = %q, want %q", doc.Content, "hello world")
	}
	if doc.Size != int64(len("hello world")) {
		t.Errorf("Size = %d, want %d", doc.Size, len("hello world"))
	}
	if doc.Version.Hash == "" {
		t.Errorf("Version.Hash is empty")
	}
	if doc.Version.Size != doc.Size {
		t.Errorf("Version.Size = %d, want %d", doc.Version.Size, doc.Size)
	}
}

func TestReadEditableFileEmptyFile(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "empty.txt"), "")

	doc, err := ReadEditableFile("empty.txt")
	if err != nil {
		t.Fatalf("ReadEditableFile: %v", err)
	}
	if doc.Content != "" {
		t.Errorf("Content = %q, want empty", doc.Content)
	}
	if doc.Size != 0 {
		t.Errorf("Size = %d, want 0", doc.Size)
	}
}

func TestReadEditableFilePreservesUnicodeAndSpecialCharacters(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	const content = "résumé 日本語\n\tline\twith\ttabs\r\nand <html> & \"quotes\"\n"

	writeFile(t, filepath.Join(root, "unicode.txt"), content)

	doc, err := ReadEditableFile("unicode.txt")
	if err != nil {
		t.Fatalf("ReadEditableFile: %v", err)
	}
	if doc.Content != content {
		t.Errorf("Content = %q, want exact preservation %q", doc.Content, content)
	}
}

func TestReadEditableFileRejectsInvalidUTF8(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.WriteFile(filepath.Join(root, "binary.txt"), []byte{0xff, 0xfe, 0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadEditableFile("binary.txt"); !errors.Is(err, ErrInvalidEncoding) {
		t.Fatalf("err = %v, want ErrInvalidEncoding", err)
	}
}

func TestReadEditableFileRejectsTooLarge(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)
	withEditLimit(t, 8)

	writeFile(t, filepath.Join(root, "big.txt"), strings.Repeat("x", 9))

	if _, err := ReadEditableFile("big.txt"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

// A file exactly at the limit is allowed; one byte more is rejected.
func TestReadEditableFileAcceptsExactlyAtLimit(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)
	withEditLimit(t, 8)

	writeFile(t, filepath.Join(root, "exact.txt"), strings.Repeat("x", 8))

	if _, err := ReadEditableFile("exact.txt"); err != nil {
		t.Fatalf("ReadEditableFile(exactly at limit) err = %v, want nil", err)
	}
}

func TestReadEditableFileMissing(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if _, err := ReadEditableFile("missing.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestReadEditableFileOutsideRoot(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if _, err := ReadEditableFile("../secret.txt"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("err = %v, want ErrAccessDenied", err)
	}
	if _, err := ReadEditableFile("/etc/passwd"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("absolute err = %v, want ErrInvalidPath", err)
	}
}

func TestReadEditableFileRejectsDirectory(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "folder.txt"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadEditableFile("folder.txt"); !errors.Is(err, ErrIsDirectory) {
		t.Fatalf("err = %v, want ErrIsDirectory", err)
	}
}

func TestReadEditableFileRejectsUnsupportedExtension(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "photo.png"), "not really an image")

	if _, err := ReadEditableFile("photo.png"); !errors.Is(err, ErrNotEditable) {
		t.Fatalf("err = %v, want ErrNotEditable", err)
	}
}

// A symlink whose final component is a link must never be followed, even when it
// points at an in-root file.
func TestReadEditableFileRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "real.txt"), "real content")
	symlinkOrSkip(t, filepath.Join(root, "real.txt"), filepath.Join(root, "link.txt"))

	if _, err := ReadEditableFile("link.txt"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("err = %v, want ErrAccessDenied", err)
	}
}

// An intermediate symlinked directory inside the root keeps the project's
// existing policy and is followed.
func TestReadEditableFileFollowsInRootSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(real, "notes.txt"), "inside")
	symlinkOrSkip(t, real, filepath.Join(root, "alias"))

	doc, err := ReadEditableFile("alias/notes.txt")
	if err != nil {
		t.Fatalf("ReadEditableFile: %v", err)
	}
	if doc.Content != "inside" {
		t.Errorf("Content = %q, want inside", doc.Content)
	}
}

func TestReadEditableFileRejectsEscapingSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(outside, "secret.txt"), "secret")
	symlinkOrSkip(t, outside, filepath.Join(root, "escape"))

	if _, err := ReadEditableFile("escape/secret.txt"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("err = %v, want ErrAccessDenied", err)
	}
}

// --- Version --------------------------------------------------------------

func TestFileVersionRoundTrip(t *testing.T) {
	v := FileVersion{Size: 42, ModTime: 123456789, Hash: strings.Repeat("a", 64)}

	parsed, ok := ParseFileVersion(v.String())
	if !ok {
		t.Fatal("ParseFileVersion returned ok=false for a valid token")
	}
	if parsed != v {
		t.Errorf("parsed = %#v, want %#v", parsed, v)
	}
}

func TestParseFileVersionRejectsMalformed(t *testing.T) {
	valid := FileVersion{Size: 1, ModTime: 2, Hash: strings.Repeat("a", 64)}.String()

	cases := []string{
		"",
		"garbage",
		"1-2",
		"1-2-3",
		"x-2-" + strings.Repeat("a", 64),
		"1-x-" + strings.Repeat("a", 64),
		"1-2-nothex",
		"-1-2-" + strings.Repeat("a", 64),
	}
	for _, tc := range cases {
		if _, ok := ParseFileVersion(tc); ok {
			t.Errorf("ParseFileVersion(%q) = ok, want false", tc)
		}
	}

	if _, ok := ParseFileVersion(valid); !ok {
		t.Errorf("ParseFileVersion(%q) = false, want true", valid)
	}
}

// The version is derived from content: changing the content changes the hash
// even if the size is identical.
func TestVersionHashDependsOnContent(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "v.txt"), "aaaa")
	first, err := ReadEditableFile("v.txt")
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(root, "v.txt"), "bbbb")
	second, err := ReadEditableFile("v.txt")
	if err != nil {
		t.Fatal(err)
	}

	if first.Version.Hash == second.Version.Hash {
		t.Errorf("version hash did not change when content changed")
	}
}

// --- Save -----------------------------------------------------------------

func TestSaveEditableFileValid(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "notes.txt"), "old")

	doc, err := ReadEditableFile("notes.txt")
	if err != nil {
		t.Fatal(err)
	}

	if err := SaveEditableFile("notes.txt", []byte("new content"), doc.Version); err != nil {
		t.Fatalf("SaveEditableFile: %v", err)
	}

	if got := readString(t, filepath.Join(root, "notes.txt")); got != "new content" {
		t.Errorf("content = %q, want %q", got, "new content")
	}
}

func TestSaveEditableFilePreservesContentExactly(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	const content = "line1\n  indented\n\ttabbed\n\ntrailing spaces   \n日本語\r\n"

	writeFile(t, filepath.Join(root, "exact.txt"), "seed")
	doc, err := ReadEditableFile("exact.txt")
	if err != nil {
		t.Fatal(err)
	}

	if err := SaveEditableFile("exact.txt", []byte(content), doc.Version); err != nil {
		t.Fatalf("SaveEditableFile: %v", err)
	}

	if got := readString(t, filepath.Join(root, "exact.txt")); got != content {
		t.Errorf("content was not preserved exactly:\ngot  %q\nwant %q", got, content)
	}
}

func TestSaveEditableFileEmptyContent(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "empty.txt"), "something")

	doc, err := ReadEditableFile("empty.txt")
	if err != nil {
		t.Fatal(err)
	}

	if err := SaveEditableFile("empty.txt", []byte(""), doc.Version); err != nil {
		t.Fatalf("SaveEditableFile: %v", err)
	}

	info, err := os.Stat(filepath.Join(root, "empty.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Errorf("size = %d, want 0", info.Size())
	}
}

func TestSaveEditableFileRejectsTooLarge(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)
	withEditLimit(t, 4)

	writeFile(t, filepath.Join(root, "small.txt"), "old")

	doc, err := ReadEditableFile("small.txt")
	if err != nil {
		t.Fatal(err)
	}

	if err := SaveEditableFile("small.txt", []byte("12345"), doc.Version); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if got := readString(t, filepath.Join(root, "small.txt")); got != "old" {
		t.Errorf("target changed to %q, want old", got)
	}
}

func TestSaveEditableFileMissingTarget(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	version := FileVersion{Size: 0, ModTime: 0, Hash: strings.Repeat("a", 64)}

	if err := SaveEditableFile("ghost.txt", []byte("x"), version); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(filepath.Join(root, "ghost.txt")); !os.IsNotExist(err) {
		t.Errorf("save recreated a missing target")
	}
}

func TestSaveEditableFileUnsupportedTarget(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "photo.png"), "data")
	version := FileVersion{Size: 4, ModTime: 0, Hash: strings.Repeat("a", 64)}

	if err := SaveEditableFile("photo.png", []byte("x"), version); !errors.Is(err, ErrNotEditable) {
		t.Fatalf("err = %v, want ErrNotEditable", err)
	}
}

func TestSaveEditableFileRejectsDirectory(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "dir.txt"), 0o755); err != nil {
		t.Fatal(err)
	}

	version := FileVersion{Hash: strings.Repeat("a", 64)}
	if err := SaveEditableFile("dir.txt", []byte("x"), version); !errors.Is(err, ErrIsDirectory) {
		t.Fatalf("err = %v, want ErrIsDirectory", err)
	}
}

func TestSaveEditableFileRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	version := FileVersion{Hash: strings.Repeat("a", 64)}
	if err := SaveEditableFile("../escape.txt", []byte("x"), version); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("err = %v, want ErrAccessDenied", err)
	}
}

func TestSaveEditableFileRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "real.txt"), "real")
	symlinkOrSkip(t, filepath.Join(root, "real.txt"), filepath.Join(root, "link.txt"))

	version := FileVersion{Hash: strings.Repeat("a", 64)}
	if err := SaveEditableFile("link.txt", []byte("hijacked"), version); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("err = %v, want ErrAccessDenied", err)
	}
	if got := readString(t, filepath.Join(root, "real.txt")); got != "real" {
		t.Errorf("link target was modified to %q", got)
	}
}

// --- Concurrency / conflict ----------------------------------------------

func TestSaveEditableFileVersionUnchangedSucceeds(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "notes.txt"), "one")

	doc, err := ReadEditableFile("notes.txt")
	if err != nil {
		t.Fatal(err)
	}

	if err := SaveEditableFile("notes.txt", []byte("two"), doc.Version); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Re-read to get the fresh version, then save again.
	doc2, err := ReadEditableFile("notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveEditableFile("notes.txt", []byte("three"), doc2.Version); err != nil {
		t.Fatalf("second save: %v", err)
	}

	if got := readString(t, filepath.Join(root, "notes.txt")); got != "three" {
		t.Errorf("content = %q, want three", got)
	}
}

// The central deterministic conflict test: open version A, change the file to
// version B on disk, then attempt to save using version A. The save must fail
// with ErrConflict and version B must remain untouched.
func TestSaveEditableFileConflictAfterExternalChange(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "notes.txt"), "version A")

	doc, err := ReadEditableFile("notes.txt")
	if err != nil {
		t.Fatal(err)
	}

	// Another writer changes the file.
	writeFile(t, filepath.Join(root, "notes.txt"), "version B")

	err = SaveEditableFile("notes.txt", []byte("version A-edited"), doc.Version)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}

	if got := readString(t, filepath.Join(root, "notes.txt")); got != "version B" {
		t.Errorf("content = %q, want version B (the other writer's change)", got)
	}
}

func TestSaveEditableFileConflictReturnsErrConflict(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "notes.txt"), "original")

	doc, err := ReadEditableFile("notes.txt")
	if err != nil {
		t.Fatal(err)
	}

	// Same size, different content: the hash must still detect the change.
	writeFile(t, filepath.Join(root, "notes.txt"), "changed!")

	if err := SaveEditableFile("notes.txt", []byte("overwrite"), doc.Version); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestSaveEditableFileConflictDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "notes.txt"), "mine")

	doc, err := ReadEditableFile("notes.txt")
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(root, "notes.txt"), "theirs")

	_ = SaveEditableFile("notes.txt", []byte("mine-edited"), doc.Version)

	if got := readString(t, filepath.Join(root, "notes.txt")); got != "theirs" {
		t.Errorf("content = %q, want theirs", got)
	}
}

// A stale version must never be accepted just because the size matches.
func TestSaveEditableFileRejectsSameSizeDifferentContent(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "n.txt"), "aaaa")
	doc, _ := ReadEditableFile("n.txt")

	writeFile(t, filepath.Join(root, "n.txt"), "bbbb")

	if err := SaveEditableFile("n.txt", []byte("cccc"), doc.Version); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

// A concurrent save test with no timing assumptions: two writers start from the
// same version. Each result must be nil or ErrConflict, the final content must be
// one of the two submissions, and no temporary file may remain. A small
// compare-then-replace window means both writers can succeed; that limitation is
// documented rather than asserted away.
func TestConcurrentSavesLeaveConsistentFile(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "shared.txt"), "seed")
	doc, err := ReadEditableFile("shared.txt")
	if err != nil {
		t.Fatal(err)
	}

	contents := []string{"writer one", "writer two"}

	var wg sync.WaitGroup
	results := make([]error, len(contents))

	for i, content := range contents {
		wg.Add(1)
		go func(i int, content string) {
			defer wg.Done()
			results[i] = SaveEditableFile("shared.txt", []byte(content), doc.Version)
		}(i, content)
	}
	wg.Wait()

	for i, err := range results {
		if err != nil && !errors.Is(err, ErrConflict) {
			t.Errorf("writer %d err = %v, want nil or ErrConflict", i, err)
		}
	}

	final := readString(t, filepath.Join(root, "shared.txt"))
	if final != contents[0] && final != contents[1] {
		t.Errorf("final content = %q, want one of the submissions", final)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), tempEditPrefix) {
			t.Errorf("temporary file left behind: %s", entry.Name())
		}
	}
}

// --- Atomicity / cleanup --------------------------------------------------

func TestSaveEditableFileLeavesNoTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "notes.txt"), "old")
	doc, err := ReadEditableFile("notes.txt")
	if err != nil {
		t.Fatal(err)
	}

	if err := SaveEditableFile("notes.txt", []byte("new"), doc.Version); err != nil {
		t.Fatalf("save: %v", err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "notes.txt" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory contains %v, want only notes.txt", names)
	}
}

// A failed replacement must not corrupt the target. Locking the directory makes
// the temporary-file creation fail before anything is written.
func TestSaveEditableFileFailureDoesNotCorruptTarget(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(locked, "notes.txt"), "precious")

	doc, err := ReadEditableFile("locked/notes.txt")
	if err != nil {
		t.Fatal(err)
	}

	lockDir(t, locked, 0o555)
	if !permissionsEnforced(t, locked) {
		t.Skip("filesystem permissions are not enforced for this user")
	}

	err = SaveEditableFile("locked/notes.txt", []byte("new"), doc.Version)
	if err == nil {
		t.Fatal("save unexpectedly succeeded in a read-only directory")
	}
	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("err = %v, want ErrAccessDenied", err)
	}

	// Restore permissions before reading so the assertion can run everywhere.
	if err := os.Chmod(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := readString(t, filepath.Join(locked, "notes.txt")); got != "precious" {
		t.Errorf("target was corrupted to %q, want precious", got)
	}
}

// The replacement is a rename, so an open handle to the old inode still sees the
// old content while the path now holds the new content. This is only meaningful
// on platforms that allow renaming over an open file.
func TestSaveEditableFileReplacesRatherThanTruncates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("renaming over an open file is not supported on Windows")
	}

	root := t.TempDir()
	withSharedRoot(t, root)

	target := filepath.Join(root, "notes.txt")
	writeFile(t, target, "old content")

	doc, err := ReadEditableFile("notes.txt")
	if err != nil {
		t.Fatal(err)
	}

	oldHandle, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer oldHandle.Close()

	if err := SaveEditableFile("notes.txt", []byte("new content"), doc.Version); err != nil {
		t.Fatalf("save: %v", err)
	}

	if got := readString(t, target); got != "new content" {
		t.Errorf("path content = %q, want new content", got)
	}

	oldBytes := make([]byte, len("old content"))
	if _, err := oldHandle.ReadAt(oldBytes, 0); err != nil {
		t.Fatalf("reading old handle: %v", err)
	}
	if string(oldBytes) != "old content" {
		t.Errorf("old inode content = %q, want old content (target was truncated in place)", string(oldBytes))
	}
}

// --- Permissions ----------------------------------------------------------

func TestSaveEditableFilePreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}

	root := t.TempDir()
	withSharedRoot(t, root)

	target := filepath.Join(root, "notes.txt")
	writeFile(t, target, "old")
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatal(err)
	}

	doc, err := ReadEditableFile("notes.txt")
	if err != nil {
		t.Fatal(err)
	}

	if err := SaveEditableFile("notes.txt", []byte("new"), doc.Version); err != nil {
		t.Fatalf("save: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("mode = %o, want 640", got)
	}
}

// An edited file must not become executable.
func TestSaveEditableFileDoesNotAddExecutableBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}

	root := t.TempDir()
	withSharedRoot(t, root)

	target := filepath.Join(root, "notes.txt")
	writeFile(t, target, "old")

	doc, err := ReadEditableFile("notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveEditableFile("notes.txt", []byte("new"), doc.Version); err != nil {
		t.Fatalf("save: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 != 0 {
		t.Errorf("mode = %o, want no executable bits", info.Mode().Perm())
	}
}
