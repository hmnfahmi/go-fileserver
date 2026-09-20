package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recursiveSearchFixture builds a nested tree for browse-level search tests.
func recursiveSearchFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "report.txt"), []byte("root report"))
	writeBytes(t, filepath.Join(root, "notes.txt"), []byte("notes"))
	writeBytes(t, filepath.Join(root, "archive", "report-2026.txt"), []byte("archive report"))
	writeBytes(t, filepath.Join(root, "archive", "old", "report-old.txt"), []byte("old report"))
	writeBytes(t, filepath.Join(root, "archive", "pic.png"), pngMagic)

	return root
}

// A recursive search renders every nested match and keeps each result's full
// relative path in the action links.
func TestBrowseRecursiveSearchRendersNestedResults(t *testing.T) {
	recursiveSearchFixture(t)

	rec, body := browseBody(t, "/?search=report")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(body, "<table") {
		t.Fatalf("recursive search did not render a table:\n%s", body)
	}

	for _, name := range []string{"report.txt", "report-2026.txt", "report-old.txt"} {
		if !strings.Contains(body, name) {
			t.Errorf("result %q missing from the page", name)
		}
	}
	if strings.Contains(body, "notes.txt") {
		t.Errorf("non-matching entry was rendered")
	}

	// Path context for nested results, using the canonical RelPath.
	if !strings.Contains(body, `class="file-path"`) {
		t.Errorf("nested results did not show path context")
	}
	if !strings.Contains(body, "archive/report-2026.txt") {
		t.Errorf("nested result did not display its relative path")
	}

	// Action links must address the real nested file, not its base name.
	if !strings.Contains(body, `href="/download?path=archive%2freport-2026.txt"`) {
		t.Errorf("nested download link does not use the full relative path")
	}
	if !strings.Contains(body, `value="archive/report-2026.txt"`) {
		t.Errorf("nested delete form does not use the full relative path")
	}
	if !strings.Contains(body, `renameFile('archive\/report-2026.txt','report-2026.txt')`) {
		t.Errorf("nested rename action does not use the full relative path")
	}
}

// Nested previewable results keep a correct preview link and directory results
// keep a correct ZIP link.
func TestBrowseRecursiveSearchActionLinksForNestedEntries(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "media", "shot-report.png"), pngMagic)
	if err := os.MkdirAll(filepath.Join(root, "bundle-report"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, body := browseBody(t, "/?search=report")

	if !strings.Contains(body, `href="/view?path=media%2fshot-report.png"`) {
		t.Errorf("nested preview link is wrong")
	}
	if !strings.Contains(body, `href="/zip?path=bundle-report"`) {
		t.Errorf("nested directory ZIP link is wrong")
	}
}

// The empty-search state from phase 5.3 is preserved when a recursive search
// finds nothing.
func TestBrowseRecursiveSearchPreservesEmptyState(t *testing.T) {
	recursiveSearchFixture(t)

	rec, body := browseBody(t, "/?search=no-such-match-xyz")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(body, "No files or folders match your search.") {
		t.Errorf("empty-search message missing:\n%s", body)
	}
	if !strings.Contains(body, `value="no-such-match-xyz"`) {
		t.Errorf("search term was not preserved")
	}
	if !strings.Contains(body, "Clear search") {
		t.Errorf("clear-search action missing")
	}
	if strings.Contains(body, "<table") {
		t.Errorf("empty search still rendered a table")
	}
	if strings.Contains(body, "This folder is empty.") {
		t.Errorf("empty search rendered the empty-directory state")
	}
}

// A search is scoped to the selected directory and keeps its breadcrumb.
func TestBrowseRecursiveSearchScopesToSelectedDirectory(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "top-report.txt"), []byte("top"))
	writeBytes(t, filepath.Join(root, "sub", "deep-report.txt"), []byte("deep"))

	rec, body := browseBody(t, "/?path=sub&search=report")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(body, "deep-report.txt") {
		t.Errorf("nested match missing")
	}
	if strings.Contains(body, "top-report.txt") {
		t.Errorf("search escaped the selected directory")
	}
	if !strings.Contains(body, `href="/?path=sub"`) {
		t.Errorf("breadcrumb to the selected directory missing")
	}
}

// With no search term the page still lists only direct children, so recursive
// traversal only happens for a search.
func TestBrowseListingWithoutSearchDoesNotRecurse(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "top.txt"), []byte("top"))
	writeBytes(t, filepath.Join(root, "sub", "nested.txt"), []byte("nested"))

	_, body := browseBody(t, "/")

	if !strings.Contains(body, "top.txt") {
		t.Errorf("direct child missing from the listing")
	}
	if strings.Contains(body, "nested.txt") {
		t.Errorf("normal listing recursed into a subdirectory")
	}
}

// A nested result found by search is still actionable: download serves the real
// file, and preview serves its content.
func TestBrowseRecursiveSearchResultIsActionable(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "archive", "report-2026.txt"), []byte("nested body"))

	dl := httptest.NewRecorder()
	Download(dl, httptest.NewRequest(http.MethodGet, "/download?path=archive%2Freport-2026.txt", nil))
	if dl.Code != http.StatusOK {
		t.Fatalf("download status = %d, want 200", dl.Code)
	}
	if dl.Body.String() != "nested body" {
		t.Errorf("download body = %q, want nested body", dl.Body.String())
	}

	pv := httptest.NewRecorder()
	View(pv, httptest.NewRequest(http.MethodGet, "/view?path=archive%2Freport-2026.txt", nil))
	if pv.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", pv.Code)
	}
	if pv.Body.String() != "nested body" {
		t.Errorf("preview body = %q, want nested body", pv.Body.String())
	}
}
