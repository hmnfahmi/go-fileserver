package handler

import (
	"archive/zip"
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newZipTestMux wires the same routes the application registers, so the tests
// exercise routing (not just handler functions).
func newZipTestMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", Browse)
	mux.HandleFunc("/view", View)
	mux.HandleFunc("/download", Download)
	mux.HandleFunc("/zip", Zip)
	mux.HandleFunc("/upload", Upload)
	mux.HandleFunc("/delete", Delete)
	mux.HandleFunc("/rename", Rename)
	return mux
}

// ZIP traversal, through the router, must be rejected and must never leak bytes.
func TestZipRoutingTraversalRejected(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	// A file that would be reachable if traversal succeeded.
	secret := filepath.Join(filepath.Dir(root), "secret.txt")
	if err := os.WriteFile(secret, []byte("topsecret"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(secret)

	mux := newZipTestMux()

	for _, target := range []string{
		"/zip?path=../secret.txt",
		"/zip?path=..%2Fsecret.txt",
		"/zip?path=sub%2F..%2F..%2Fsecret.txt",
		"/zip?path=%2Fetc",
		"/zip?path=C%3A%5CWindows",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

		if rec.Code == http.StatusOK {
			t.Errorf("%s: status = 200, want rejection", target)
		}
		if bytes.Contains(rec.Body.Bytes(), []byte("topsecret")) {
			t.Errorf("%s: traversal leaked file content", target)
		}
	}
}

// The directory ZIP link must actually download an archive through the router,
// and the archive must be openable by a standard extractor.
func TestZipRoutingDownloadsArchive(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "bundle", "a.txt"), []byte("alpha"))
	writeBytes(t, filepath.Join(root, "bundle", "nested", "b.txt"), []byte("beta"))

	mux := newZipTestMux()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/zip?path=bundle", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := rec.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("downloaded archive is not readable: %v", err)
	}

	contents := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %q: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %q: %v", f.Name, err)
		}
		contents[f.Name] = string(data)
	}

	if contents["a.txt"] != "alpha" {
		t.Errorf("a.txt = %q, want alpha", contents["a.txt"])
	}
	if contents["nested/b.txt"] != "beta" {
		t.Errorf("nested/b.txt = %q, want beta", contents["nested/b.txt"])
	}
}

// Scope I regression: the existing download endpoint still streams a file
// unchanged after the ZIP route was added.
func TestDownloadRegressionWithZipRoute(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "keep.txt"), []byte("keep-me"))

	mux := newZipTestMux()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/download?path=keep.txt", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "keep-me" {
		t.Errorf("download body = %q, want keep-me", got)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "keep.txt") {
		t.Errorf("Content-Disposition = %q, want keep.txt", cd)
	}
}

// Scope I regression: upload, rename, delete and browse search still work
// alongside the ZIP route.
func TestUploadRenameDeleteRegressionWithZipRoute(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	mux := newZipTestMux()

	// Upload.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "uploaded.txt")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write([]byte("uploaded"))
	mw.Close()

	upReq := httptest.NewRequest(http.MethodPost, "/upload?path=", &body)
	upReq.Header.Set("Content-Type", mw.FormDataContentType())
	upRec := httptest.NewRecorder()
	mux.ServeHTTP(upRec, upReq)

	if upRec.Code != http.StatusSeeOther && upRec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want 303 or 200", upRec.Code)
	}
	if got := readFileString(t, filepath.Join(root, "uploaded.txt")); got != "uploaded" {
		t.Fatalf("uploaded.txt = %q, want uploaded", got)
	}

	// Rename.
	renReq := httptest.NewRequest(http.MethodPost, "/rename", strings.NewReader("path=uploaded.txt&newname=renamed.txt"))
	renReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	renRec := httptest.NewRecorder()
	mux.ServeHTTP(renRec, renReq)

	if renRec.Code != http.StatusSeeOther && renRec.Code != http.StatusOK {
		t.Fatalf("rename status = %d", renRec.Code)
	}
	if got := readFileString(t, filepath.Join(root, "renamed.txt")); got != "uploaded" {
		t.Fatalf("renamed.txt = %q, want uploaded", got)
	}

	// Browse search still scopes to filenames.
	searchRec := httptest.NewRecorder()
	mux.ServeHTTP(searchRec, httptest.NewRequest(http.MethodGet, "/?search=renamed", nil))
	if !strings.Contains(searchRec.Body.String(), "renamed.txt") {
		t.Errorf("search did not find renamed.txt")
	}

	// Delete.
	delReq := httptest.NewRequest(http.MethodPost, "/delete", strings.NewReader("path=renamed.txt"))
	delReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	delRec := httptest.NewRecorder()
	mux.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusSeeOther && delRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d", delRec.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); !os.IsNotExist(err) {
		t.Errorf("renamed.txt still exists after delete")
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
