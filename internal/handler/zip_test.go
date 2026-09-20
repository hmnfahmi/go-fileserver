package handler

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"go-fileserver/internal/service"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipFromRecorder decodes the archive captured in a response recorder.
func zipFromRecorder(t *testing.T, rec *httptest.ResponseRecorder) *zip.Reader {
	t.Helper()

	body := rec.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("response is not a valid ZIP: %v", err)
	}
	return zr
}

func zipNames(zr *zip.Reader) []string {
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}

func zipContent(t *testing.T, zr *zip.Reader, name string) string {
	t.Helper()

	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %q: %v", name, err)
		}
		defer rc.Close()

		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read %q: %v", name, err)
		}
		return string(data)
	}

	t.Fatalf("entry %q missing from %v", name, zipNames(zr))
	return ""
}

// A directory downloads as a valid ZIP that a real extractor can open, with the
// expected entries and contents.
func TestZipDirectoryProducesOpenableArchive(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "docs", "readme.txt"), []byte("hello"))
	writeBytes(t, filepath.Join(root, "docs", "sub", "deep.txt"), []byte("deep"))
	writeBytes(t, filepath.Join(root, "docs", "blob.bin"), []byte{0x00, 0xff, 0x10})

	rec := httptest.NewRecorder()
	Zip(rec, httptest.NewRequest(http.MethodGet, "/zip?path=docs", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "docs.zip") {
		t.Errorf("Content-Disposition = %q, want docs.zip", cd)
	}
	if rec.Header().Get("Content-Length") != "" {
		t.Errorf("Content-Length should be omitted for a streamed archive, got %q", rec.Header().Get("Content-Length"))
	}

	zr := zipFromRecorder(t, rec)
	names := zipNames(zr)

	for _, want := range []string{"readme.txt", "sub/", "sub/deep.txt", "blob.bin"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("entry %q missing from %v", want, names)
		}
	}

	if got := zipContent(t, zr, "readme.txt"); got != "hello" {
		t.Errorf("readme.txt = %q, want hello", got)
	}
	if got := zipContent(t, zr, "sub/deep.txt"); got != "deep" {
		t.Errorf("sub/deep.txt = %q, want deep", got)
	}
}

// Every entry name must be relative, contain no traversal and no backslash.
func TestZipEntryNamesAreSafe(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "tree", "a", "b", "c.txt"), []byte("c"))
	writeBytes(t, filepath.Join(root, "tree", "top.txt"), []byte("t"))

	rec := httptest.NewRecorder()
	Zip(rec, httptest.NewRequest(http.MethodGet, "/zip?path=tree", nil))

	for _, name := range zipNames(zipFromRecorder(t, rec)) {
		if strings.HasPrefix(name, "/") {
			t.Errorf("entry %q is absolute", name)
		}
		if strings.Contains(name, `\`) {
			t.Errorf("entry %q contains a backslash", name)
		}
		for _, part := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
			if part == ".." || part == "." {
				t.Errorf("entry %q contains a traversal component", name)
			}
		}
	}
}

// Unicode and special-character filenames must be preserved inside the archive.
func TestZipPreservesSpecialFilenames(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	special := []string{"a&b c.txt", "日本語.txt", "plus+sign.txt", "hash#tag.txt", "query?.txt"}
	for _, n := range special {
		writeBytes(t, filepath.Join(root, "sp", n), []byte("x"))
	}

	rec := httptest.NewRecorder()
	Zip(rec, httptest.NewRequest(http.MethodGet, "/zip?path=sp", nil))

	got := zipNames(zipFromRecorder(t, rec))
	for _, want := range special {
		found := false
		for _, n := range got {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("entry %q missing from %v", want, got)
		}
	}
}

// The listing must render an encoded ZIP link for a directory with special
// characters, and it must round-trip back to the same directory.
func TestZipLinkIsEncodedForSpecialDirectoryName(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.MkdirAll(filepath.Join(root, "a&b c"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeBytes(t, filepath.Join(root, "a&b c", "f.txt"), []byte("x"))

	rec := httptest.NewRecorder()
	Browse(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	// The href must be URL-encoded: a literal "&" or space would break it.
	if strings.Contains(body, `href="/zip?path=a&b c"`) {
		t.Fatalf("zip link is not URL-encoded: %q", body)
	}
	if !strings.Contains(body, "a%26b%20c") {
		t.Fatalf("expected encoded directory name in zip link")
	}

	// The encoded link must resolve to the directory and archive its contents.
	zipRec := httptest.NewRecorder()
	Zip(zipRec, httptest.NewRequest(http.MethodGet, "/zip?path=a%26b%20c", nil))

	if zipRec.Code != http.StatusOK {
		t.Fatalf("encoded zip request status = %d, want 200", zipRec.Code)
	}
	if got := zipContent(t, zipFromRecorder(t, zipRec), "f.txt"); got != "x" {
		t.Errorf("f.txt = %q, want x", got)
	}
}

// Symlinks must never be archived, in or out of root, dangling or not.
func TestZipSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	writeBytes(t, filepath.Join(outside, "secret.txt"), []byte("secret"))
	writeBytes(t, filepath.Join(root, "src", "real.txt"), []byte("real"))

	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "src", "escape.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "src", "real.txt"), filepath.Join(root, "src", "inroot.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("does-not-exist", filepath.Join(root, "src", "dangling.txt")); err != nil {
		t.Fatal(err)
	}

	withSharedRoot(t, root)

	rec := httptest.NewRecorder()
	Zip(rec, httptest.NewRequest(http.MethodGet, "/zip?path=src", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	names := zipNames(zipFromRecorder(t, rec))
	if len(names) != 1 || names[0] != "real.txt" {
		t.Fatalf("entries = %v, want only [real.txt]", names)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("secret")) {
		t.Error("archive leaked the external symlink target")
	}
}

// A symlink that escapes the root and is selected directly must be forbidden.
func TestZipRejectsEscapingSymlinkedDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "inside"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "inside", "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	withSharedRoot(t, root)

	rec := httptest.NewRecorder()
	Zip(rec, httptest.NewRequest(http.MethodGet, "/zip?path=inside%2Fescape", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(outside)) {
		t.Error("response leaked the filesystem path")
	}
}

// Missing, traversal and method are handled with the established semantics.
func TestZipErrorAndMethodHandling(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)
	writeBytes(t, filepath.Join(root, "f.txt"), []byte("x"))

	cases := []struct {
		name       string
		method     string
		target     string
		wantStatus int
	}{
		{"missing", http.MethodGet, "/zip?path=ghost", http.StatusNotFound},
		{"traversal", http.MethodGet, "/zip?path=..%2Fsecret", http.StatusForbidden},
		{"encoded traversal", http.MethodGet, "/zip?path=..%2F..%2Fetc", http.StatusForbidden},
		{"method post", http.MethodPost, "/zip?path=f.txt", http.StatusMethodNotAllowed},
		{"method delete", http.MethodDelete, "/zip?path=f.txt", http.StatusMethodNotAllowed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			Zip(rec, httptest.NewRequest(tc.method, tc.target, nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

// A HEAD request must return the archive headers without a body.
func TestZipHeadHasNoBody(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)
	writeBytes(t, filepath.Join(root, "docs", "f.txt"), []byte("x"))

	rec := httptest.NewRecorder()
	Zip(rec, httptest.NewRequest(http.MethodHead, "/zip?path=docs", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("HEAD body length = %d, want 0", rec.Body.Len())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "docs.zip") {
		t.Errorf("Content-Disposition = %q, want docs.zip", cd)
	}
}

// A regular file can be zipped as a single entry.
func TestZipRegularFile(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)
	writeBytes(t, filepath.Join(root, "one.txt"), []byte("only"))

	rec := httptest.NewRecorder()
	Zip(rec, httptest.NewRequest(http.MethodGet, "/zip?path=one.txt", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	zr := zipFromRecorder(t, rec)
	if names := zipNames(zr); len(names) != 1 || names[0] != "one.txt" {
		t.Fatalf("entries = %v, want [one.txt]", names)
	}
	if got := zipContent(t, zr, "one.txt"); got != "only" {
		t.Errorf("content = %q, want only", got)
	}
}

// The root directory archives its contents.
func TestZipRootDirectory(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)
	writeBytes(t, filepath.Join(root, "a.txt"), []byte("a"))
	writeBytes(t, filepath.Join(root, "sub", "b.txt"), []byte("b"))

	rec := httptest.NewRecorder()
	Zip(rec, httptest.NewRequest(http.MethodGet, "/zip?path=", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	names := strings.Join(zipNames(zipFromRecorder(t, rec)), ",")
	for _, want := range []string{"a.txt", "sub/", "sub/b.txt"} {
		if !strings.Contains(names, want) {
			t.Errorf("entry %q missing from %q", want, names)
		}
	}
}

// The listing offers ZIP download for directories only; files keep the existing
// preview/download/rename/delete actions.
func TestBrowseZipActionRenderedForDirectoriesOnly(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeBytes(t, filepath.Join(root, "file.txt"), []byte("x"))

	rec := httptest.NewRecorder()
	Browse(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()

	if !strings.Contains(body, `href="/zip?path=folder"`) {
		t.Errorf("directory is missing its ZIP link")
	}
	if strings.Contains(body, `href="/zip?path=file.txt"`) {
		t.Errorf("regular file was offered a ZIP link")
	}
	if !strings.Contains(body, `href="/download?path=file.txt"`) {
		t.Errorf("regular file lost its download link")
	}
}

// An empty directory must produce a valid, empty archive rather than an error.
func TestZipEmptyDirectoryProducesEmptyArchive(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	Zip(rec, httptest.NewRequest(http.MethodGet, "/zip?path=empty", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if names := zipNames(zipFromRecorder(t, rec)); len(names) != 0 {
		t.Errorf("entries = %v, want none", names)
	}
}

// A non-regular target (a Unix socket) is not archivable; the handler must
// reject it as a bad request instead of streaming a broken archive.
func TestZipRejectsNonRegularTarget(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	ln, err := net.Listen("unix", filepath.Join(root, "sock"))
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	defer ln.Close()

	rec := httptest.NewRecorder()
	Zip(rec, httptest.NewRequest(http.MethodGet, "/zip?path=sock", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// A directory that cannot be read must fail with 403 before any bytes are
// streamed, and the body must not leak the internal path or syscall detail.
func TestZipPermissionDeniedIsForbidden(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	// Skip when the process can still read the directory (e.g. running as root).
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("filesystem permissions are not enforced for this user")
	}

	rec := httptest.NewRecorder()
	Zip(rec, httptest.NewRequest(http.MethodGet, "/zip?path=locked", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, root) || strings.Contains(body, "permission denied") {
		t.Errorf("response leaked internal detail: %q", body)
	}
}

// A file that vanishes after the entry list is built must not be presented as a
// success when nothing has been written yet: streamZip reports a clean 404.
func TestStreamZipCleanErrorBeforeStreaming(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/zip")
	rec.Header().Set("Content-Disposition", `attachment; filename="gone.txt.zip"`)

	entries := []service.ZipEntry{{Name: "gone.txt", Target: filepath.Join(t.TempDir(), "missing.txt")}}
	streamZip(rec, entries)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct == "application/zip" {
		t.Errorf("archive Content-Type was not replaced on a pre-stream failure: %q", ct)
	}
	if rec.Header().Get("Content-Disposition") != "" {
		t.Errorf("Content-Disposition should be cleared on a pre-stream failure, got %q", rec.Header().Get("Content-Disposition"))
	}
	if body := rec.Body.String(); strings.Contains(body, "zip") || strings.Contains(body, ".txt") {
		t.Errorf("clean error body should not mention archive internals: %q", body)
	}
}

// Once bytes have been written the status cannot change; a mid-stream failure
// must leave a truncated (non-openable) archive rather than a fake success.
func TestStreamZipMidFailureLeavesTruncatedArchive(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present.txt")
	// Incompressible data forces zip.Writer's internal buffer to flush to the
	// response before the missing file is reached, which is what makes the
	// failure happen after streaming has started.
	payload := make([]byte, 128*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(present, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	entries := []service.ZipEntry{
		{Name: "present.txt", Target: present},
		{Name: "missing.txt", Target: filepath.Join(dir, "missing.txt")},
	}

	rec := httptest.NewRecorder()
	streamZip(rec, entries)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (already streaming)", rec.Code)
	}
	body := rec.Body.Bytes()
	if len(body) == 0 {
		t.Fatal("expected the first entry to have been streamed")
	}
	if _, err := zip.NewReader(bytes.NewReader(body), int64(len(body))); err == nil {
		t.Error("truncated archive is not openable by a standard extractor")
	}
}
