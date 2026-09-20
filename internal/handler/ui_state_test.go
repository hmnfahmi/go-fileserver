package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// browseBody runs Browse and returns the recorder plus the rendered body.
func browseBody(t *testing.T, target string) (*httptest.ResponseRecorder, string) {
	t.Helper()

	rec := httptest.NewRecorder()
	Browse(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec, rec.Body.String()
}

// An empty directory must render an explanatory empty state instead of an empty
// table, while keeping the breadcrumb and an obvious upload action.
func TestBrowseEmptyDirectoryRendersEmptyState(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	rec, body := browseBody(t, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(body, "This folder is empty.") {
		t.Errorf("empty-state message missing:\n%s", body)
	}
	if !strings.Contains(body, "There are no files or folders here yet.") {
		t.Errorf("empty-state explanatory text missing")
	}
	if strings.Contains(body, "<table") {
		t.Errorf("empty directory still rendered a table")
	}
	if !strings.Contains(body, `href="#upload-zone"`) {
		t.Errorf("empty directory did not offer the upload action")
	}
	if !strings.Contains(body, `class="breadcrumb"`) {
		t.Errorf("breadcrumb missing from the empty directory state")
	}
}

// An empty subdirectory keeps the user oriented with its breadcrumb.
func TestBrowseEmptySubdirectoryRendersBreadcrumb(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.MkdirAll(filepath.Join(root, "docs", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	rec, body := browseBody(t, "/?path=docs%2Fempty")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(body, "This folder is empty.") {
		t.Errorf("empty subdirectory state missing")
	}
	if !strings.Contains(body, `href="/?path=docs"`) {
		t.Errorf("breadcrumb link to the parent directory missing")
	}
	if !strings.Contains(body, "empty") {
		t.Errorf("breadcrumb does not name the current directory")
	}
}

// An empty search result is semantically different from an empty directory and
// must render its own message, preserve the term and offer a way to clear it.
func TestBrowseEmptySearchRendersNoResultsState(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, body := browseBody(t, "/?search=nomatch")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(body, "No files or folders match your search.") {
		t.Errorf("no-results message missing:\n%s", body)
	}
	if strings.Contains(body, "This folder is empty.") {
		t.Errorf("empty search result used the empty-directory message")
	}
	if strings.Contains(body, "<table") {
		t.Errorf("empty search result still rendered a table")
	}

	// The search term must be preserved in the input and echoed in the message.
	if !strings.Contains(body, `value="nomatch"`) {
		t.Errorf("search term was not preserved in the search input")
	}
	if !strings.Contains(body, "nomatch") {
		t.Errorf("search term was not shown in the no-results message")
	}

	// A clear-search action must point back at the current directory.
	if !strings.Contains(body, "Clear search") {
		t.Errorf("no-results state did not offer a clear-search action")
	}
	if !strings.Contains(body, `href="/?path="`) {
		t.Errorf("clear-search link does not target the current directory")
	}
}

// A search that matches keeps the normal table, so the no-results state is not
// shown for a non-empty result set.
func TestBrowseSearchWithMatchesRendersTable(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, body := browseBody(t, "/?search=readme")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(body, "<table") {
		t.Errorf("matching search did not render the table")
	}
	if !strings.Contains(body, "readme.txt") {
		t.Errorf("matching search did not render the matched file")
	}
	if strings.Contains(body, "No files or folders match your search.") {
		t.Errorf("matching search wrongly rendered the no-results state")
	}
	if !strings.Contains(body, `value="readme"`) {
		t.Errorf("search term was not preserved")
	}
}

// The empty-directory and empty-search states must not be collapsed into one.
func TestBrowseEmptySearchAndEmptyDirectoryAreDistinct(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	_, emptyDir := browseBody(t, "/")
	_, emptySearch := browseBody(t, "/?search=anything")

	if !strings.Contains(emptyDir, "This folder is empty.") {
		t.Errorf("empty directory did not render the directory message")
	}
	if strings.Contains(emptyDir, "No files or folders match your search.") {
		t.Errorf("empty directory rendered the search message")
	}
	if !strings.Contains(emptySearch, "No files or folders match your search.") {
		t.Errorf("empty search did not render the search message")
	}
	if strings.Contains(emptySearch, "This folder is empty.") {
		t.Errorf("empty search rendered the directory message")
	}
}

// A search term containing HTML must be escaped, not injected, in the state.
func TestBrowseEmptySearchEscapesTerm(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	_, body := browseBody(t, "/?search=%3Cscript%3Ealert(1)%3C%2Fscript%3E")

	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatalf("search term was injected unescaped into the page")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("search term was not rendered in escaped form")
	}
}

// A normal non-empty directory must still render the table and its actions.
func TestBrowseNonEmptyDirectoryStillRendersTable(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, body := browseBody(t, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(body, "<table") {
		t.Errorf("non-empty directory did not render a table")
	}
	if !strings.Contains(body, "folder") || !strings.Contains(body, "file.txt") {
		t.Errorf("non-empty directory did not render its entries")
	}
	if !strings.Contains(body, `href="/zip?path=folder"`) {
		t.Errorf("directory ZIP action missing")
	}
	if !strings.Contains(body, `href="/download?path=file.txt"`) {
		t.Errorf("file download action missing")
	}
	if strings.Contains(body, "This folder is empty.") {
		t.Errorf("non-empty directory rendered the empty state")
	}
}

// Browse errors must keep their HTTP status and render the shared HTML error
// page with a navigation action, never a 200 and never an internal path.
func TestBrowseErrorsRenderHTMLAndPreserveStatus(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		target     string
		wantStatus int
		wantTitle  string
	}{
		{"not found", "/?path=missing", http.StatusNotFound, "Page not found"},
		{"traversal", "/?path=..%2Fsecret", http.StatusForbidden, "Access denied"},
		{"not a directory", "/?path=file.txt", http.StatusBadRequest, "Bad request"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, body := browseBody(t, tc.target)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("Content-Type = %q, want text/html", ct)
			}
			if !strings.Contains(body, tc.wantTitle) {
				t.Errorf("error page missing title %q:\n%s", tc.wantTitle, body)
			}
			if !strings.Contains(body, `href="/"`) {
				t.Errorf("error page did not offer a navigation action")
			}
			if strings.Contains(body, root) {
				t.Errorf("error page leaked the shared root path")
			}
			if strings.Contains(body, "This folder is empty.") {
				t.Errorf("error page rendered an empty-state message")
			}
		})
	}
}

// The error page must never echo raw OS error detail.
func TestBrowseErrorPageDoesNotLeakSyscallDetail(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	_, body := browseBody(t, "/?path=missing")

	for _, forbidden := range []string{"no such file", "open ", "stat ", "syscall"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Errorf("error page contains raw OS detail %q:\n%s", forbidden, body)
		}
	}
}

// writeHTMLServiceError must render only the sanitised public message: a raw error
// carrying an absolute path and syscall text must not reach the page.
func TestWriteBrowseErrorSanitisesRawError(t *testing.T) {
	cases := []struct {
		name        string
		err         error
		wantStatus  int
		wantMessage string
	}{
		{
			name:        "permission",
			err:         fmt.Errorf("open /srv/private/data: %w", os.ErrPermission),
			wantStatus:  http.StatusForbidden,
			wantMessage: "Access denied",
		},
		{
			name:        "generic",
			err:         errors.New("read /srv/private/data: disk on fire"),
			wantStatus:  http.StatusInternalServerError,
			wantMessage: "Operation failed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeHTMLServiceError(rec, tc.err)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}

			body := rec.Body.String()
			if strings.Contains(body, "/srv/private/data") {
				t.Errorf("error page leaked the raw path:\n%s", body)
			}
			if strings.Contains(body, "permission denied") || strings.Contains(body, "disk on fire") {
				t.Errorf("error page leaked the raw error text:\n%s", body)
			}
			if !strings.Contains(body, tc.wantMessage) {
				t.Errorf("error page missing public message %q:\n%s", tc.wantMessage, body)
			}
		})
	}
}

// A successful Browse must not be affected by the error page's status handling:
// it stays a 200.
func TestBrowseSuccessRemainsStatusOK(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	rec, _ := browseBody(t, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// The preview endpoint is also a browser navigation surface, so its failures
// render the same HTML error page and keep the original status.
func TestViewErrorsRenderHTMLAndPreserveStatus(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "album.png"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.bin"), []byte("not previewable"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		target     string
		wantStatus int
		wantTitle  string
	}{
		{"not found", "/view?path=ghost.txt", http.StatusNotFound, "Page not found"},
		{"traversal", "/view?path=..%2Fsecret.txt", http.StatusForbidden, "Access denied"},
		{"directory", "/view?path=album.png", http.StatusBadRequest, "Bad request"},
		{"unsupported type", "/view?path=note.bin", http.StatusUnsupportedMediaType, "Unsupported file type"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			View(rec, httptest.NewRequest(http.MethodGet, tc.target, nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("Content-Type = %q, want text/html", ct)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.wantTitle) {
				t.Errorf("error page missing title %q:\n%s", tc.wantTitle, body)
			}
			if !strings.Contains(body, `href="/"`) {
				t.Errorf("error page did not offer a navigation action")
			}
			if strings.Contains(body, root) {
				t.Errorf("error page leaked the shared root path")
			}
		})
	}
}
