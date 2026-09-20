package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// createTestMux wires the create routes together with browse so a successful
// creation can be verified by loading the directory again.
func createTestMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", Browse)
	mux.HandleFunc("/mkdir", CreateFolder)
	mux.HandleFunc("/create-file", CreateFile)
	return mux
}

func postCreateForm(target string, values url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestCreateFolderSucceeds(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	rec := httptest.NewRecorder()
	CreateFolder(rec, postCreateForm("/mkdir", url.Values{"path": {""}, "name": {"reports"}}))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (body %q)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/?path=" {
		t.Errorf("Location = %q, want /?path=", loc)
	}

	info, err := os.Stat(filepath.Join(root, "reports"))
	if err != nil || !info.IsDir() {
		t.Fatalf("folder was not created (err=%v)", err)
	}
}

func TestCreateFileSucceeds(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	rec := httptest.NewRecorder()
	CreateFile(rec, postCreateForm("/create-file", url.Values{"path": {""}, "name": {"notes.txt"}}))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (body %q)", rec.Code, rec.Body.String())
	}

	info, err := os.Stat(filepath.Join(root, "notes.txt"))
	if err != nil {
		t.Fatalf("file was not created: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("size = %d, want a zero-byte file", info.Size())
	}
}

func TestCreateRedirectsToCurrentDirectory(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.MkdirAll(filepath.Join(root, "documents", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	CreateFolder(rec, postCreateForm("/mkdir", url.Values{"path": {"documents/projects"}, "name": {"reports"}}))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/?path=documents%2Fprojects" {
		t.Errorf("Location = %q, want /?path=documents%%2Fprojects", loc)
	}
}

// The redirect must survive every character that is special in a URL.
func TestCreateRedirectEncodesSpecialDirectoryNames(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	dirs := []string{"a b", "a&b", "a#b", "a%b", "a+b", "日本語", "a=b"}

	for _, dir := range dirs {
		t.Run(dir, func(t *testing.T) {
			if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
				t.Fatal(err)
			}

			rec := httptest.NewRecorder()
			CreateFile(rec, postCreateForm("/create-file", url.Values{"path": {dir}, "name": {"x.txt"}}))

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
			if params := parsed.Query(); len(params) != 1 {
				t.Errorf("Location %q produced %d query parameters, want 1", loc, len(params))
			}

			if _, err := os.Stat(filepath.Join(root, dir, "x.txt")); err != nil {
				t.Errorf("file was not created in %q: %v", dir, err)
			}
		})
	}
}

func TestCreateRejectsDisallowedMethods(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	cases := []struct {
		name   string
		call   func(http.ResponseWriter, *http.Request)
		method string
	}{
		{"mkdir GET", CreateFolder, http.MethodGet},
		{"mkdir HEAD", CreateFolder, http.MethodHead},
		{"mkdir DELETE", CreateFolder, http.MethodDelete},
		{"mkdir PUT", CreateFolder, http.MethodPut},
		{"create-file GET", CreateFile, http.MethodGet},
		{"create-file HEAD", CreateFile, http.MethodHead},
		{"create-file DELETE", CreateFile, http.MethodDelete},
		{"create-file PUT", CreateFile, http.MethodPut},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/?path=&name=made", nil)
			rec := httptest.NewRecorder()

			tc.call(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 405", rec.Code)
			}
			if allow := rec.Header().Get("Allow"); allow != http.MethodPost {
				t.Errorf("Allow = %q, want POST", allow)
			}
		})
	}

	if _, err := os.Stat(filepath.Join(root, "made")); !os.IsNotExist(err) {
		t.Errorf("a rejected request still created an entry")
	}
}

func TestCreateRejectsInvalidCurrentPath(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	rec := httptest.NewRecorder()
	CreateFolder(rec, postCreateForm("/mkdir", url.Values{"path": {"../outside"}, "name": {"sub"}}))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if strings.Contains(rec.Body.String(), root) {
		t.Errorf("error response leaked the shared root path")
	}
}

func TestCreateRejectsInvalidName(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	rec := httptest.NewRecorder()
	CreateFile(rec, postCreateForm("/create-file", url.Values{"path": {""}, "name": {"sub/notes.txt"}}))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if strings.Contains(rec.Body.String(), root) {
		t.Errorf("error response leaked the shared root path")
	}
	if _, err := os.Stat(filepath.Join(root, "notes.txt")); !os.IsNotExist(err) {
		t.Errorf("an invalid name silently created a different file")
	}
}

func TestCreateConflictIs409(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.WriteFile(filepath.Join(root, "existing.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
		req  *http.Request
	}{
		{"file conflict", CreateFile, postCreateForm("/create-file", url.Values{"path": {""}, "name": {"existing.txt"}})},
		{"folder conflict", CreateFolder, postCreateForm("/mkdir", url.Values{"path": {""}, "name": {"existing.txt"}})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.call(rec, tc.req)

			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "File or folder already exists") {
				t.Errorf("conflict body = %q, want the public conflict message", rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), root) {
				t.Errorf("conflict response leaked the shared root path")
			}
		})
	}

	if got := readFileString(t, filepath.Join(root, "existing.txt")); got != "keep" {
		t.Errorf("existing file changed to %q, want keep", got)
	}
}

func TestCreateMissingParentIs404(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	rec := httptest.NewRecorder()
	CreateFile(rec, postCreateForm("/create-file", url.Values{"path": {"missing"}, "name": {"a.txt"}}))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), root) {
		t.Errorf("error response leaked the shared root path")
	}
}

func TestCreateEscapingSymlinkedParentIs403(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	rec := httptest.NewRecorder()
	CreateFile(rec, postCreateForm("/create-file", url.Values{"path": {"escape"}, "name": {"a.txt"}}))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(outside, "a.txt")); !os.IsNotExist(err) {
		t.Errorf("creation escaped the shared root")
	}
}

// writeCreateError must never echo a raw error and must map unexpected failures
// to a sanitised 500.
func TestWriteCreateErrorSanitisesRawError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeCreateError(rec, errors.New("mkdir /srv/private/data: disk on fire"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	body := rec.Body.String()
	if strings.Contains(body, "/srv/private/data") || strings.Contains(body, "disk on fire") {
		t.Errorf("error response leaked raw detail: %q", body)
	}
	if !strings.Contains(body, "Operation failed") {
		t.Errorf("body = %q, want the public fallback message", body)
	}
}

// A successful creation must be visible on the next browse of the same
// directory.
func TestCreatedItemBecomesVisible(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	mux := createTestMux()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, postCreateForm("/mkdir", url.Values{"path": {""}, "name": {"visible-dir"}}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("mkdir status = %d, want 303", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, postCreateForm("/create-file", url.Values{"path": {""}, "name": {"visible.txt"}}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create-file status = %d, want 303", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "visible-dir") || !strings.Contains(body, "visible.txt") {
		t.Errorf("created items are not visible on the next browse:\n%s", body)
	}
}
