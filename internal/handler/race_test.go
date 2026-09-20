package handler

import (
	"errors"
	"go-fileserver/internal/model"
	"go-fileserver/internal/service"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F4.4. Uploading into a directory that disappeared after resolution must map to
// the documented not-found behaviour rather than an unexpected failure. In JSON
// mode the API envelope is preserved and the reason appears in the failed list.
func TestUploadIntoMissingDirectory(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	req := multipartUploadRequest(t, "missingdir", "application/json",
		map[string]string{"file.txt": "data"})

	rec := httptest.NewRecorder()
	Upload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (JSON envelope preserved)", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "File or folder not found") {
		t.Errorf("expected a not-found reason in the response, got %q", body)
	}
	if strings.Contains(body, root) {
		t.Errorf("response leaked the absolute root path: %q", body)
	}
}

// A browser-mode upload into a missing directory must not redirect as though it
// succeeded.
func TestUploadIntoMissingDirectoryBrowserModeErrors(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	req := multipartUploadRequest(t, "missingdir", "text/html", map[string]string{
		"file.txt": "data",
	})

	rec := httptest.NewRecorder()
	Upload(rec, req)

	if rec.Code == http.StatusSeeOther {
		t.Fatalf("browser upload redirected despite never writing the file")
	}
	if rec.Code < 400 {
		t.Fatalf("status = %d, want an error status", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "file.txt")); err == nil {
		t.Fatal("file was unexpectedly created at the root")
	}
}

// permissionDeniedDir makes dir read-only and restores the mode on cleanup so
// t.TempDir can remove it afterwards.
func permissionDeniedDir(t *testing.T, dir string) {
	t.Helper()

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
}

func permissionEnforced(t *testing.T, dir string) bool {
	t.Helper()

	probe := filepath.Join(dir, ".probe")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		f.Close()
		os.Remove(probe)
		return false
	}

	return errors.Is(err, os.ErrPermission)
}

// A denied delete must be a 403 with a client-safe body, never a raw syscall
// string or an internal path.
func TestDeletePermissionDeniedIsForbidden(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	permissionDeniedDir(t, locked)
	if !permissionEnforced(t, locked) {
		t.Skip("filesystem permissions are not enforced for this user")
	}

	req := httptest.NewRequest(http.MethodPost, "/delete?path=locked/a.txt", nil)
	rec := httptest.NewRecorder()

	Delete(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, root) || strings.Contains(body, "permission denied") {
		t.Errorf("response leaked internal detail: %q", body)
	}
}

// The sentinels produced by classifyFSError at the service layer must surface as
// the matching HTTP status through the shared handler mapper, keeping the two
// layers consistent.
func TestClassifiedServiceErrorsMapToStatuses(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	// List needs a read-denied directory (missing the read bit) to fail, while
	// Delete needs a write-denied directory (missing the write bit).
	hidden := filepath.Join(root, "hidden")
	if err := os.Mkdir(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(hidden, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(hidden, 0o755) })

	permissionDeniedDir(t, locked)
	if !permissionEnforced(t, locked) {
		t.Skip("filesystem permissions are not enforced for this user")
	}

	_, listErr := service.List(model.ListOptions{Path: "hidden"})
	deleteErr := service.Delete("locked/a.txt")

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"denied list", listErr, http.StatusForbidden},
		{"denied delete", deleteErr, http.StatusForbidden},
	}

	for _, tc := range cases {
		if got := statusForError(tc.err); got != tc.want {
			t.Errorf("%s: statusForError(%v) = %d, want %d", tc.name, tc.err, got, tc.want)
		}
	}
}
