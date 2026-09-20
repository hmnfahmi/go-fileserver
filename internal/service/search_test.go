package service

import (
	"context"
	"errors"
	"go-fileserver/internal/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// searchRelPaths runs a search and returns the canonical relative paths of the
// results, which is the value every action link is built from.
func searchRelPaths(t *testing.T, opts model.ListOptions) []string {
	t.Helper()

	items, err := List(opts)
	if err != nil {
		t.Fatalf("List(%+v): %v", opts, err)
	}

	paths := make([]string, 0, len(items))
	for _, item := range items {
		if strings.HasPrefix(item.RelPath, "/") || strings.Contains(item.RelPath, "..") {
			t.Errorf("result RelPath is not a contained relative path: %q", item.RelPath)
		}
		paths = append(paths, item.RelPath)
	}
	return paths
}

// searchFixture builds the tree from the phase example:
//
//	report.txt
//	notes.txt
//	report-dir/
//	archive/report-2026.txt
//	archive/old/report-old.txt
func searchFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "report.txt"), "root report")
	writeFile(t, filepath.Join(root, "notes.txt"), "notes")
	writeFile(t, filepath.Join(root, "archive", "report-2026.txt"), "archive report")
	writeFile(t, filepath.Join(root, "archive", "old", "report-old.txt"), "old report")

	if err := os.MkdirAll(filepath.Join(root, "report-dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	return root
}

// A search from the root must reach direct, nested and deeply nested matches,
// keep each result's canonical relative path and still put directories first.
func TestSearchRecursiveFindsNestedMatches(t *testing.T) {
	searchFixture(t)

	got := searchRelPaths(t, model.ListOptions{Search: "report"})
	want := []string{
		"report-dir",
		"archive/report-2026.txt",
		"archive/old/report-old.txt",
		"report.txt",
	}

	if !equalStrings(got, want) {
		t.Errorf("recursive search = %v, want %v", got, want)
	}
}

// A directory whose name matches is itself a result, not just a container.
func TestSearchRecursiveMatchesDirectories(t *testing.T) {
	searchFixture(t)

	items, err := List(model.ListOptions{Search: "report-dir"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(items), items)
	}
	if !items[0].IsDir {
		t.Errorf("directory match was not reported as a directory: %+v", items[0])
	}
	if items[0].RelPath != "report-dir" {
		t.Errorf("RelPath = %q, want report-dir", items[0].RelPath)
	}
}

// Matching stays case-insensitive in both directions: a lower-case term matches
// an upper-case name, and an upper-case term matches a lower-case name.
func TestSearchRecursiveIsCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "lower-report.txt"), "x")
	writeFile(t, filepath.Join(root, "UPPER-REPORT.TXT"), "x")

	want := []string{"lower-report.txt", "UPPER-REPORT.TXT"}

	got := searchRelPaths(t, model.ListOptions{Search: "report"})
	if !equalStrings(got, want) {
		t.Errorf("lower-case term = %v, want %v", got, want)
	}

	got = searchRelPaths(t, model.ListOptions{Search: "REPORT"})
	if !equalStrings(got, want) {
		t.Errorf("upper-case term = %v, want %v", got, want)
	}
}

// Matching stays substring-based.
func TestSearchRecursiveIsSubstring(t *testing.T) {
	searchFixture(t)

	got := searchRelPaths(t, model.ListOptions{Search: "port"})
	want := []string{
		"report-dir",
		"archive/report-2026.txt",
		"archive/old/report-old.txt",
		"report.txt",
	}

	if !equalStrings(got, want) {
		t.Errorf("substring search = %v, want %v", got, want)
	}
}

// A term with no match returns an empty, non-error result.
func TestSearchRecursiveNoMatch(t *testing.T) {
	searchFixture(t)

	got := searchRelPaths(t, model.ListOptions{Search: "zzz-no-such-name"})
	if len(got) != 0 {
		t.Errorf("no-match search returned %v, want empty", got)
	}
}

// Matching is against the base name only, never the accumulated path.
func TestSearchRecursiveMatchesBaseNameNotPath(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "match-dir", "unrelated.txt"), "x")

	got := searchRelPaths(t, model.ListOptions{Search: "match-dir"})
	want := []string{"match-dir"}

	if !equalStrings(got, want) {
		t.Errorf("search = %v, want %v (the nested file's base name does not match)", got, want)
	}
}

// Unicode and shell-special names are matched and keep their exact RelPath.
func TestSearchRecursiveUnicodeAndSpecialCharacters(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "docs", "résumé-ünïcode.txt"), "x")
	writeFile(t, filepath.Join(root, "docs", "nested", "résumé-deep.txt"), "x")
	writeFile(t, filepath.Join(root, "docs", "a b&c[1].txt"), "x")

	got := searchRelPaths(t, model.ListOptions{Search: "résumé"})
	want := []string{"docs/nested/résumé-deep.txt", "docs/résumé-ünïcode.txt"}
	if !equalStrings(got, want) {
		t.Errorf("unicode search = %v, want %v", got, want)
	}

	got = searchRelPaths(t, model.ListOptions{Search: "b&c"})
	want = []string{"docs/a b&c[1].txt"}
	if !equalStrings(got, want) {
		t.Errorf("special-character search = %v, want %v", got, want)
	}
}

// A nested match must keep its full relative path so actions operate on the
// real file rather than a same-named sibling.
func TestSearchRecursivePreservesNestedRelPath(t *testing.T) {
	searchFixture(t)

	items, err := List(model.ListOptions{Search: "report-2026"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d results, want 1", len(items))
	}
	if items[0].Name != "report-2026.txt" {
		t.Errorf("Name = %q, want report-2026.txt", items[0].Name)
	}
	if items[0].RelPath != "archive/report-2026.txt" {
		t.Errorf("RelPath = %q, want archive/report-2026.txt", items[0].RelPath)
	}
}

// Searching a subdirectory must stay scoped to that subtree.
func TestSearchRecursiveScopesToSelectedDirectory(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "top-report.txt"), "top")
	writeFile(t, filepath.Join(root, "sub", "deep-report.txt"), "deep")
	writeFile(t, filepath.Join(root, "sibling", "other-report.txt"), "sibling")

	got := searchRelPaths(t, model.ListOptions{Path: "sub", Search: "report"})
	want := []string{"sub/deep-report.txt"}
	if !equalStrings(got, want) {
		t.Errorf("scoped search = %v, want %v", got, want)
	}

	got = searchRelPaths(t, model.ListOptions{Search: "report"})
	want = []string{"sub/deep-report.txt", "sibling/other-report.txt", "top-report.txt"}
	if !equalStrings(got, want) {
		t.Errorf("root search = %v, want %v", got, want)
	}
}

// A search with no term must not recurse: normal browsing is unchanged.
func TestSearchEmptyDoesNotRecurse(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "top.txt"), "x")
	writeFile(t, filepath.Join(root, "sub", "nested.txt"), "x")

	got := searchRelPaths(t, model.ListOptions{})
	want := []string{"sub", "top.txt"}
	if !equalStrings(got, want) {
		t.Errorf("empty search = %v, want direct children %v", got, want)
	}
}

// The same filesystem state must always produce the same order, even when two
// directories hold an identically named entry. The relative-path tiebreak makes
// the order independent of depth-first traversal order: "ab!" sorts before "ab"
// as a directory entry, but "ab!/x.txt" sorts before "ab/x.txt" as a path.
func TestSearchRecursiveOrderIsDeterministic(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "ab", "x.txt"), "1")
	writeFile(t, filepath.Join(root, "ab!", "x.txt"), "2")

	want := []string{"ab!/x.txt", "ab/x.txt"}
	first := searchRelPaths(t, model.ListOptions{Search: "x.txt"})
	if !equalStrings(first, want) {
		t.Fatalf("search order = %v, want %v", first, want)
	}

	for i := 0; i < 5; i++ {
		got := searchRelPaths(t, model.ListOptions{Search: "x.txt"})
		if !equalStrings(got, first) {
			t.Fatalf("run %d order = %v, want stable %v", i, got, first)
		}
	}
}

// A cancelled request stops the walk instead of completing a large search.
func TestSearchRecursiveStopsOnCancelledContext(t *testing.T) {
	searchFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := List(model.ListOptions{Search: "report", Context: ctx})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("List(cancelled) err = %v, want context.Canceled", err)
	}
}

// The walk checks the context before reading a directory, so cancellation is
// honoured even when the selected directory is empty and the per-entry check
// never runs.
func TestSearchRecursiveStopsOnCancelledContextEmptyRoot(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := List(model.ListOptions{Search: "anything", Context: ctx})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("List(cancelled, empty root) err = %v, want context.Canceled", err)
	}
}

// An escaping search root is rejected by the existing boundary before any walk
// starts.
func TestSearchRecursiveRejectsEscapingRoot(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if _, err := List(model.ListOptions{Path: "../", Search: "x"}); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("List(path=..) err = %v, want ErrAccessDenied", err)
	}
}

// A search root that is a symlink escaping the shared root must be rejected, so
// the recursive walk can never begin outside the root.
func TestSearchRecursiveRejectsEscapingSymlinkRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(outside, "outside-report.txt"), "secret")
	symlinkOrSkip(t, outside, filepath.Join(root, "escape"))

	if _, err := List(model.ListOptions{Path: "escape", Search: "report"}); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("List(escaping symlink root) err = %v, want ErrAccessDenied", err)
	}
}

// Descendant symlinks and junctions are never followed and never reported,
// matching the archive walker. In-root, escaping and dangling links all behave
// the same: only the genuine file is returned.
func TestSearchRecursiveSkipsDescendantLinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "real", "target-report.txt"), "real")
	writeFile(t, filepath.Join(outside, "outside-report.txt"), "secret")
	writeFile(t, filepath.Join(outside, "outside-dir", "deep-report.txt"), "secret")

	// A. descendant symlink to a regular file inside the shared root.
	symlinkOrSkip(t, filepath.Join(root, "real", "target-report.txt"), filepath.Join(root, "link-report.txt"))
	// B. descendant symlink to a directory inside the shared root.
	symlinkOrSkip(t, filepath.Join(root, "real"), filepath.Join(root, "linkdir-report"))
	// C. descendant symlink escaping the shared root.
	symlinkOrSkip(t, filepath.Join(outside, "outside-report.txt"), filepath.Join(root, "escape-report.txt"))
	// D. descendant symlink to a directory outside the shared root.
	symlinkOrSkip(t, outside, filepath.Join(root, "escapedir-report"))
	// E. dangling symlink.
	symlinkOrSkip(t, filepath.Join(root, "does-not-exist-report.txt"), filepath.Join(root, "dangling-report.txt"))

	got := searchRelPaths(t, model.ListOptions{Search: "report"})
	want := []string{"real/target-report.txt"}

	if !equalStrings(got, want) {
		t.Errorf("search followed or reported a link: got %v, want %v", got, want)
	}
}

// An unreadable subtree is skipped, so one inaccessible directory does not turn
// an otherwise useful search into a failure. The readable match must survive.
func TestSearchRecursiveSkipsUnreadableSubtree(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "visible", "visible-report.txt"), "ok")

	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(locked, "hidden-report.txt"), "secret")
	lockDir(t, locked, 0o000)
	if !permissionsEnforced(t, locked) {
		t.Skip("filesystem permissions are not enforced for this user")
	}

	got := searchRelPaths(t, model.ListOptions{Search: "report"})
	want := []string{"visible/visible-report.txt"}

	if !equalStrings(got, want) {
		t.Errorf("search = %v, want %v (unreadable subtree should be skipped)", got, want)
	}
}

// walkSearch must never report or descend into an entry that is not contained by
// the shared root, even if it is ever handed a directory outside it. List
// validates the selected root first, so this is defense-in-depth.
func TestWalkSearchRejectsOutOfRootEntries(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(outside, "outside-report.txt"), "secret")
	writeFile(t, filepath.Join(outside, "outside-report-dir", "deep-report.txt"), "secret")

	var out []model.FileItem
	if err := walkSearch(context.Background(), root, outside, "", "report", &out); err != nil {
		t.Fatalf("walkSearch: %v", err)
	}

	if len(out) != 0 {
		t.Fatalf("walkSearch reported %d entries from outside the root: %+v", len(out), out)
	}
}
