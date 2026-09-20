package handler

import (
	"errors"
	"go-fileserver/internal/config"
	"go-fileserver/internal/service"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// withEditLimit overrides config.MaxEditSize for the duration of the test.
func withEditLimit(t *testing.T, n int64) {
	t.Helper()

	original := config.MaxEditSize
	config.MaxEditSize = n
	t.Cleanup(func() { config.MaxEditSize = original })
}

func postEditForm(values url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/edit", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

var versionPattern = regexp.MustCompile(`name="version" value="([^"]+)"`)

// editVersion loads the editor page and returns the version token the server
// embedded, so tests can perform a real open-then-save round trip.
func editVersion(t *testing.T, target string) (string, string) {
	t.Helper()

	rec := httptest.NewRecorder()
	Edit(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200 (body %q)", target, rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	match := versionPattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("editor page did not contain a version token:\n%s", body)
	}

	return match[1], body
}

func TestEditGetRendersContent(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "notes.txt"), []byte("# Title\n\nBody text\n"))

	rec := httptest.NewRecorder()
	Edit(rec, httptest.NewRequest(http.MethodGet, "/edit?path=notes.txt", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}

	body := rec.Body.String()
	for _, want := range []string{"Edit: notes.txt", "# Title", "Body text", `name="path" value="notes.txt"`, `id="edit-content"`, `id="edit-save"`, "Save", "Cancel"} {
		if !strings.Contains(body, want) {
			t.Errorf("editor page missing %q", want)
		}
	}
	if !versionPattern.MatchString(body) {
		t.Errorf("editor page missing the version field")
	}
}

// File content must be HTML-escaped and its whitespace preserved exactly.
func TestEditGetEscapesHTMLAndPreservesWhitespace(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	const content = "<script>alert('x')</script>\n\tindented\n  spaced  \n"

	writeBytes(t, filepath.Join(root, "page.html"), []byte(content))

	rec := httptest.NewRecorder()
	Edit(rec, httptest.NewRequest(http.MethodGet, "/edit?path=page.html", nil))

	body := rec.Body.String()
	if strings.Contains(body, "<script>alert('x')</script>") {
		t.Fatalf("file content was injected unescaped")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("file content was not rendered in escaped form")
	}
	if !strings.Contains(body, "\n\tindented\n  spaced  \n") {
		t.Errorf("whitespace was not preserved exactly")
	}
}

func TestEditGetRejectsMissing(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	rec := httptest.NewRecorder()
	Edit(rec, httptest.NewRequest(http.MethodGet, "/edit?path=ghost.txt", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), root) {
		t.Errorf("response leaked the shared root path")
	}
}

func TestEditGetRejectsDirectory(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "folder.txt"), 0o755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	Edit(rec, httptest.NewRequest(http.MethodGet, "/edit?path=folder.txt", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestEditGetRejectsUnsupportedExtension(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "photo.png"), []byte("not an image"))

	rec := httptest.NewRecorder()
	Edit(rec, httptest.NewRequest(http.MethodGet, "/edit?path=photo.png", nil))

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func TestEditGetRejectsInvalidUTF8(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "binary.txt"), []byte{0xff, 0xfe, 0x00})

	rec := httptest.NewRecorder()
	Edit(rec, httptest.NewRequest(http.MethodGet, "/edit?path=binary.txt", nil))

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func TestEditGetRejectsTooLarge(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)
	withEditLimit(t, 8)

	writeBytes(t, filepath.Join(root, "big.txt"), []byte(strings.Repeat("x", 9)))

	rec := httptest.NewRecorder()
	Edit(rec, httptest.NewRequest(http.MethodGet, "/edit?path=big.txt", nil))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestEditGetRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	rec := httptest.NewRecorder()
	Edit(rec, httptest.NewRequest(http.MethodGet, "/edit?path=..%2Fsecret.txt", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if strings.Contains(rec.Body.String(), root) {
		t.Errorf("response leaked the shared root path")
	}
}

func TestEditGetRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(outside, "secret.txt"), []byte("secret"))
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	rec := httptest.NewRecorder()
	Edit(rec, httptest.NewRequest(http.MethodGet, "/edit?path=escape%2Fsecret.txt", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestEditGetRejectsSymlinkFinalComponent(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "real.txt"), []byte("real"))
	if err := os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	rec := httptest.NewRecorder()
	Edit(rec, httptest.NewRequest(http.MethodGet, "/edit?path=link.txt", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestEditRejectsDisallowedMethods(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			Edit(rec, httptest.NewRequest(method, "/edit?path=notes.txt", nil))

			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 405", rec.Code)
			}
			if allow := rec.Header().Get("Allow"); allow != "GET, HEAD, POST" {
				t.Errorf("Allow = %q, want \"GET, HEAD, POST\"", allow)
			}
		})
	}
}

func TestEditPostSavesContent(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	target := filepath.Join(root, "notes.txt")
	writeBytes(t, target, []byte("old content"))

	version, _ := editVersion(t, "/edit?path=notes.txt")

	rec := httptest.NewRecorder()
	Edit(rec, postEditForm(url.Values{
		"path":    {"notes.txt"},
		"version": {version},
		"content": {"new content"},
	}))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (body %q)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/?path=" {
		t.Errorf("Location = %q, want /?path=", loc)
	}
	if got := readFileString(t, target); got != "new content" {
		t.Errorf("content = %q, want new content", got)
	}
}

func TestEditPostRedirectsToCurrentDirectory(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.MkdirAll(filepath.Join(root, "documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeBytes(t, filepath.Join(root, "documents", "notes.txt"), []byte("x"))

	version, _ := editVersion(t, "/edit?path=documents%2Fnotes.txt")

	rec := httptest.NewRecorder()
	Edit(rec, postEditForm(url.Values{
		"path":    {"documents/notes.txt"},
		"version": {version},
		"content": {"y"},
	}))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/?path=documents" {
		t.Errorf("Location = %q, want /?path=documents", loc)
	}
}

func TestEditPostRedirectEncodesSpecialDirectoryNames(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	for _, dir := range []string{"a b", "a&b", "a#b", "a%b", "a+b", "日本語"} {
		t.Run(dir, func(t *testing.T) {
			if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
				t.Fatal(err)
			}
			writeBytes(t, filepath.Join(root, dir, "notes.txt"), []byte("x"))

			version, _ := editVersion(t, "/edit?path="+url.QueryEscape(dir+"/notes.txt"))

			rec := httptest.NewRecorder()
			Edit(rec, postEditForm(url.Values{
				"path":    {dir + "/notes.txt"},
				"version": {version},
				"content": {"y"},
			}))

			if rec.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303 (body %q)", rec.Code, rec.Body.String())
			}

			loc := rec.Header().Get("Location")
			parsed, err := url.Parse(loc)
			if err != nil {
				t.Fatalf("Location %q is not a valid URL: %v", loc, err)
			}
			if got := parsed.Query().Get("path"); got != dir {
				t.Errorf("redirect path = %q, want %q (Location %q)", got, dir, loc)
			}
		})
	}
}

func TestEditPostRejectsInvalidPath(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "notes.txt"), []byte("x"))
	version, _ := editVersion(t, "/edit?path=notes.txt")

	rec := httptest.NewRecorder()
	Edit(rec, postEditForm(url.Values{
		"path":    {"../escape.txt"},
		"version": {version},
		"content": {"x"},
	}))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if strings.Contains(rec.Body.String(), root) {
		t.Errorf("response leaked the shared root path")
	}
}

func TestEditPostRejectsInvalidVersion(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "notes.txt"), []byte("x"))

	rec := httptest.NewRecorder()
	Edit(rec, postEditForm(url.Values{
		"path":    {"notes.txt"},
		"version": {"garbage"},
		"content": {"y"},
	}))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestEditPostRejectsMissingFile(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	version := service.FileVersion{Hash: strings.Repeat("a", 64)}.String()

	rec := httptest.NewRecorder()
	Edit(rec, postEditForm(url.Values{
		"path":    {"ghost.txt"},
		"version": {version},
		"content": {"y"},
	}))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestEditPostConflictIs409(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	target := filepath.Join(root, "notes.txt")
	writeBytes(t, target, []byte("version A"))

	version, _ := editVersion(t, "/edit?path=notes.txt")

	// Another writer changes the file after it was opened.
	writeBytes(t, target, []byte("version B"))

	rec := httptest.NewRecorder()
	Edit(rec, postEditForm(url.Values{
		"path":    {"notes.txt"},
		"version": {version},
		"content": {"version A edited"},
	}))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %q)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "File changed since you opened it. Your changes were not saved.") {
		t.Errorf("conflict page missing the explicit message: %s", rec.Body.String())
	}
	if got := readFileString(t, target); got != "version B" {
		t.Errorf("content = %q, want version B", got)
	}
	if strings.Contains(rec.Body.String(), root) {
		t.Errorf("conflict response leaked the shared root path")
	}
}

func TestEditPostRejectsTooLarge(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)
	withEditLimit(t, 4)

	writeBytes(t, filepath.Join(root, "small.txt"), []byte("old"))
	version, _ := editVersion(t, "/edit?path=small.txt")

	rec := httptest.NewRecorder()
	Edit(rec, postEditForm(url.Values{
		"path":    {"small.txt"},
		"version": {version},
		"content": {"12345"},
	}))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if got := readFileString(t, filepath.Join(root, "small.txt")); got != "old" {
		t.Errorf("content = %q, want old", got)
	}
}

// A raw error passed to writeEditError must never reach the page.
func TestWriteEditErrorSanitisesRawError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeEditError(rec, errors.New("write /srv/private/data: disk on fire"), "notes.txt")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	body := rec.Body.String()
	if strings.Contains(body, "/srv/private/data") || strings.Contains(body, "disk on fire") {
		t.Errorf("error page leaked raw detail: %q", body)
	}
	if !strings.Contains(body, "Operation failed") {
		t.Errorf("body = %q, want the public fallback message", body)
	}
}

// The conflict page offers a way back to the folder that holds the file.
func TestWriteEditErrorConflictOffersBackLink(t *testing.T) {
	rec := httptest.NewRecorder()
	writeEditError(rec, service.ErrConflict, "documents/notes.txt")

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "File changed since you opened it. Your changes were not saved.") {
		t.Errorf("conflict message missing: %s", body)
	}
	if !strings.Contains(body, `href="/?path=documents"`) {
		t.Errorf("conflict page missing a link back to the folder: %s", body)
	}
	if !strings.Contains(body, `href="/"`) {
		t.Errorf("conflict page missing the home link")
	}
}

// A successful save must be immediately visible through Browse.
func TestEditSaveBecomesVisibleOnBrowse(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "notes.txt"), []byte("before"))
	version, _ := editVersion(t, "/edit?path=notes.txt")

	rec := httptest.NewRecorder()
	Edit(rec, postEditForm(url.Values{
		"path":    {"notes.txt"},
		"version": {version},
		"content": {"after"},
	}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save status = %d, want 303", rec.Code)
	}

	rec = httptest.NewRecorder()
	Edit(rec, httptest.NewRequest(http.MethodGet, "/edit?path=notes.txt", nil))
	if !strings.Contains(rec.Body.String(), "after") {
		t.Errorf("saved content is not visible on the editor page")
	}
}
