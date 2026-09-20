package service

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipEntryNames returns the set of entry names in a streamed archive.
func zipEntryNames(t *testing.T, data []byte) []string {
	t.Helper()

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}

	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}

// zipEntryData extracts one entry's contents.
func zipEntryData(t *testing.T, data []byte, name string) []byte {
	t.Helper()

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}

	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open entry %q: %v", name, err)
		}
		defer rc.Close()

		got, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read entry %q: %v", name, err)
		}
		return got
	}

	t.Fatalf("entry %q not found in %v", name, zipEntryNames(t, data))
	return nil
}

// buildZip runs the full service path (prepare + stream) and returns the bytes.
func buildZip(t *testing.T, rel string) ([]byte, string) {
	t.Helper()

	entries, download, err := PrepareZip(rel)
	if err != nil {
		t.Fatalf("PrepareZip(%q): %v", rel, err)
	}

	var buf bytes.Buffer
	if err := WriteZip(&buf, entries); err != nil {
		t.Fatalf("WriteZip: %v", err)
	}
	return buf.Bytes(), download
}

// 1 + 2 + 6 + 18: a normal directory with files, a nested directory and a binary
// file must round-trip through a real archive.
func TestPrepareZipAndWriteZipRoundTrip(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	binary := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0x00, 0x7f, 0x80}
	writeFile(t, filepath.Join(root, "docs", "readme.txt"), "hello world")
	writeFile(t, filepath.Join(root, "docs", "nested", "deep.txt"), "deep")
	if err := os.WriteFile(filepath.Join(root, "docs", "blob.bin"), binary, 0o644); err != nil {
		t.Fatal(err)
	}

	data, download := buildZip(t, "docs")

	if download != "docs" {
		t.Errorf("download name = %q, want %q", download, "docs")
	}

	names := zipEntryNames(t, data)
	want := map[string]bool{
		"readme.txt":      true,
		"blob.bin":        true,
		"nested/":         true,
		"nested/deep.txt": true,
	}
	for name := range want {
		found := false
		for _, got := range names {
			if got == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("entry %q missing from %v", name, names)
		}
	}

	if got := string(zipEntryData(t, data, "readme.txt")); got != "hello world" {
		t.Errorf("readme.txt = %q, want %q", got, "hello world")
	}
	if got := string(zipEntryData(t, data, "nested/deep.txt")); got != "deep" {
		t.Errorf("nested/deep.txt = %q, want %q", got, "deep")
	}
	if got := zipEntryData(t, data, "blob.bin"); !bytes.Equal(got, binary) {
		t.Errorf("blob.bin = %v, want %v", got, binary)
	}
}

// 3: an empty directory yields a valid, openable archive with a single
// directory entry.
func TestPrepareZipEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	data, download := buildZip(t, "empty")

	if download != "empty" {
		t.Errorf("download name = %q, want empty", download)
	}
	names := zipEntryNames(t, data)
	if len(names) != 0 {
		t.Errorf("entries = %v, want none (the directory's own contents)", names)
	}

	// The archive must still be a valid, openable ZIP.
	if _, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("empty archive is not readable: %v", err)
	}
}

// 4: Unicode filenames survive.
func TestPrepareZipUnicodeFilename(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	name := "日本語 ファイル.txt"
	writeFile(t, filepath.Join(root, "uni", name), "unicode")

	data, _ := buildZip(t, "uni")

	names := zipEntryNames(t, data)
	if len(names) != 1 || names[0] != name {
		t.Fatalf("entries = %v, want [%q]", names, name)
	}
	if got := string(zipEntryData(t, data, name)); got != "unicode" {
		t.Errorf("content = %q, want unicode", got)
	}
}

// 5: special characters, including spaces, &, #, ?, % and +, are preserved
// literally in the entry name (only the URL encoding differs).
func TestPrepareZipSpecialCharacterFilename(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	names := []string{
		"a&b c.txt",
		"hash#tag.txt",
		"query?.txt",
		"percent%.txt",
		"plus+sign.txt",
		`quote"double.txt`,
		"single'quote.txt",
	}

	for _, n := range names {
		writeFile(t, filepath.Join(root, "special", n), "x")
	}

	data, _ := buildZip(t, "special")
	got := zipEntryNames(t, data)

	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected entry %q", g)
		}
		delete(want, g)
	}
	for missing := range want {
		t.Errorf("entry %q missing from %v", missing, got)
	}
}

// 7: the root directory itself archives every top-level entry.
func TestPrepareZipRootDirectory(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "a.txt"), "a")
	writeFile(t, filepath.Join(root, "sub", "b.txt"), "b")

	data, download := buildZip(t, "")

	if download != filepath.Base(root) && download != "shared" {
		t.Errorf("download name = %q, want the root directory name or %q", download, "shared")
	}

	names := zipEntryNames(t, data)
	joined := strings.Join(names, ",")
	for _, want := range []string{"a.txt", "sub/", "sub/b.txt"} {
		if !strings.Contains(joined, want) {
			t.Errorf("entry %q missing from %v", want, names)
		}
	}
}

// 8: a regular file is archived as a single entry named after the file.
func TestPrepareZipRegularFile(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "single.txt"), "just me")

	data, download := buildZip(t, "single.txt")

	if download != "single.txt" {
		t.Errorf("download name = %q, want single.txt", download)
	}
	names := zipEntryNames(t, data)
	if len(names) != 1 || names[0] != "single.txt" {
		t.Fatalf("entries = %v, want [single.txt]", names)
	}
	if got := string(zipEntryData(t, data, "single.txt")); got != "just me" {
		t.Errorf("content = %q, want %q", got, "just me")
	}
}

// 9: a missing path is a clean not-found.
func TestPrepareZipMissingPath(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if _, _, err := PrepareZip("ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// 10: traversal is rejected.
func TestPrepareZipTraversal(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	for _, rel := range []string{"..", "../", "../outside", "a/../../outside"} {
		if _, _, err := PrepareZip(rel); !errors.Is(err, ErrAccessDenied) {
			t.Errorf("PrepareZip(%q) err = %v, want ErrAccessDenied", rel, err)
		}
	}
}

// 11: a selected directory reached through a symlink that escapes the root is
// rejected.
func TestPrepareZipRejectsEscapingSelectedDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "inside"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, outside, filepath.Join(root, "inside", "escape"))

	withSharedRoot(t, root)

	if _, _, err := PrepareZip("inside/escape"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("err = %v, want ErrAccessDenied", err)
	}
}

// 12 + 13 + 14: symlinks (escaping, in-root and dangling) are never archived.
func TestPrepareZipSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	writeFile(t, filepath.Join(outside, "secret.txt"), "secret")
	writeFile(t, filepath.Join(root, "src", "real.txt"), "real")

	symlinkOrSkip(t, filepath.Join(outside, "secret.txt"), filepath.Join(root, "src", "escape.txt"))
	symlinkOrSkip(t, filepath.Join(root, "src", "real.txt"), filepath.Join(root, "src", "inroot.txt"))
	symlinkOrSkip(t, filepath.Join(root, "src", "does-not-exist"), filepath.Join(root, "src", "dangling.txt"))

	withSharedRoot(t, root)

	data, _ := buildZip(t, "src")

	names := zipEntryNames(t, data)
	if len(names) != 1 || names[0] != "real.txt" {
		t.Fatalf("entries = %v, want only [real.txt]", names)
	}
	if bytes.Contains(data, []byte("secret")) {
		t.Error("archive contains the external symlink target's content")
	}
}

// A symlink to a directory must not be descended into.
func TestPrepareZipSkipsSymlinkedDirectory(t *testing.T) {
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "src", "realdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "src", "realdir", "inside.txt"), "inside")

	symlinkOrSkip(t, filepath.Join(root, "src", "realdir"), filepath.Join(root, "src", "linkdir"))

	withSharedRoot(t, root)

	data, _ := buildZip(t, "src")
	names := zipEntryNames(t, data)

	for _, name := range names {
		if strings.HasPrefix(name, "linkdir") {
			t.Errorf("symlinked directory was archived: %v", names)
		}
	}
}

// 15 + 16 + 17: every entry name is relative, contains no "..", and carries no
// Windows separator.
func TestPrepareZipEntryNamesAreSafe(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "tree", "a", "b", "c.txt"), "c")
	writeFile(t, filepath.Join(root, "tree", "top.txt"), "top")

	data, _ := buildZip(t, "tree")

	for _, name := range zipEntryNames(t, data) {
		if strings.HasPrefix(name, "/") {
			t.Errorf("entry %q is absolute", name)
		}
		if len(name) >= 2 && name[1] == ':' {
			t.Errorf("entry %q carries a drive designator", name)
		}
		if strings.Contains(name, `\`) {
			t.Errorf("entry %q contains a backslash", name)
		}
		for _, part := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
			if part == ".." || part == "." {
				t.Errorf("entry %q contains a traversal component", name)
			}
			if part == "" {
				t.Errorf("entry %q contains an empty component", name)
			}
		}
	}
}

// A filename containing a literal backslash on POSIX must not leak a Windows
// separator into the archive.
func TestPrepareZipBackslashFilenameIsNeutralised(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	name := `a\b.txt`
	if err := os.MkdirAll(filepath.Join(root, "bs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bs", name), []byte("x"), 0o644); err != nil {
		t.Skipf("backslash in filename unsupported: %v", err)
	}

	data, _ := buildZip(t, "bs")

	names := zipEntryNames(t, data)
	if len(names) != 1 || names[0] != "a_b.txt" {
		t.Fatalf("entries = %v, want [a_b.txt]", names)
	}
	if got := string(zipEntryData(t, data, "a_b.txt")); got != "x" {
		t.Errorf("content = %q, want x", got)
	}
}

// A backslash filename sanitises to "_", which is not injective: "a\b.txt" and
// "a_b.txt" both map to "a_b.txt". The archive must never contain a duplicate
// entry, and the literal name must win deterministically.
func TestPrepareZipBackslashCollisionIsResolved(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	dir := filepath.Join(root, "collide")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	literal := "a_b.txt"
	backslash := `a\b.txt`

	if err := os.WriteFile(filepath.Join(dir, literal), []byte("literal"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, backslash), []byte("backslash"), 0o644); err != nil {
		t.Skipf("backslash in filename unsupported: %v", err)
	}

	// Run the walk repeatedly: the winner must be deterministic.
	for i := 0; i < 5; i++ {
		data, _ := buildZip(t, "collide")

		names := zipEntryNames(t, data)
		seen := map[string]int{}
		for _, n := range names {
			seen[n]++
		}
		for n, c := range seen {
			if c != 1 {
				t.Fatalf("entry %q appears %d times in %v", n, c, names)
			}
		}

		// Exactly one of the two files survives, and it is the literal name.
		if len(names) != 1 || names[0] != literal {
			t.Fatalf("entries = %v, want exactly [%q]", names, literal)
		}
		if got := string(zipEntryData(t, data, literal)); got != "literal" {
			t.Fatalf("entry %q = %q, want %q", literal, got, "literal")
		}
	}
}

// Duplicate names cannot arise from a real filesystem listing, but the walker
// must still emit each entry exactly once.
func TestPrepareZipNoDuplicateEntries(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "dup", "same.txt"), "x")

	data, _ := buildZip(t, "dup")
	names := zipEntryNames(t, data)

	seen := map[string]int{}
	for _, n := range names {
		seen[n]++
	}
	for n, c := range seen {
		if c != 1 {
			t.Errorf("entry %q appears %d times", n, c)
		}
	}
}

// WriteZip must report a file that vanished between PrepareZip and streaming
// rather than emitting a silently truncated archive.
func TestWriteZipMissingFileReturnsNotFound(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "gone", "f.txt"), "data")

	entries, _, err := PrepareZip("gone")
	if err != nil {
		t.Fatalf("PrepareZip: %v", err)
	}

	// Simulate a file disappearing after the entry list was built.
	if err := os.Remove(filepath.Join(root, "gone", "f.txt")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := WriteZip(&buf, entries); !errors.Is(err, ErrNotFound) {
		t.Fatalf("WriteZip err = %v, want ErrNotFound", err)
	}
}

// The archive base name follows the logical name the user selected, even when
// that directory is reached through a symlink.
func TestPrepareZipDownloadNameUsesLogicalName(t *testing.T) {
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "real", "f.txt"), "x")
	symlinkOrSkip(t, filepath.Join(root, "real"), filepath.Join(root, "alias"))

	withSharedRoot(t, root)

	_, download, err := PrepareZip("alias")
	if err != nil {
		t.Fatalf("PrepareZip(alias): %v", err)
	}
	if download != "alias" {
		t.Errorf("download name = %q, want %q", download, "alias")
	}
}

// A non-regular target (here a Unix socket) has no portable archive
// representation and must be rejected instead of being read or followed.
func TestPrepareZipRejectsNonRegularTarget(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	sock := filepath.Join(root, "sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	defer ln.Close()

	if _, _, err := PrepareZip("sock"); !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("err = %v, want ErrNotDirectory", err)
	}
}

// A regular file and a directory are both accepted: a file as one entry, a
// directory as its own contents.
func TestPrepareZipAcceptsFileAndDirectory(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "f.txt"), "x")
	if err := os.MkdirAll(filepath.Join(root, "d"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, _, err := PrepareZip("f.txt"); err != nil {
		t.Fatalf("regular file unexpectedly rejected: %v", err)
	}
	if _, _, err := PrepareZip("d"); err != nil {
		t.Fatalf("directory unexpectedly rejected: %v", err)
	}
}
