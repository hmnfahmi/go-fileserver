package handler

import (
	"go-fileserver/internal/config"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withSharedRoot(t *testing.T, dir string) {
	t.Helper()

	originalPath := config.SharedPath
	originalPreview := config.MaxPreviewSize
	originalUpload := config.MaxUploadSize

	config.SharedPath = dir
	config.MaxPreviewSize = 1024 * 1024
	config.MaxUploadSize = 1024 * 1024

	t.Cleanup(func() {
		config.SharedPath = originalPath
		config.MaxPreviewSize = originalPreview
		config.MaxUploadSize = originalUpload
	})
}

func TestBrowseRejectsNonGet(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()

	Browse(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
		t.Errorf("Allow = %q, want to contain GET", allow)
	}
}

func TestDownloadRejectsNonGet(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/download", nil)
	rec := httptest.NewRecorder()

	Download(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestViewRejectsNonGet(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/view", nil)
	rec := httptest.NewRecorder()

	View(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestDeleteRejectsNonPost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/delete", nil)
	rec := httptest.NewRecorder()

	Delete(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != http.MethodPost {
		t.Errorf("Allow = %q, want %q", allow, http.MethodPost)
	}
}

func TestRenameRejectsNonPost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/rename", nil)
	rec := httptest.NewRecorder()

	Rename(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestBrowseRejectsEscapeWithForbidden(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	req := httptest.NewRequest(http.MethodGet, "/?path=../secret", nil)
	rec := httptest.NewRecorder()

	Browse(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if strings.Contains(rec.Body.String(), root) {
		t.Errorf("response leaked internal path: %q", rec.Body.String())
	}
}

func TestBrowseEscapesFolderLinks(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "a&b"), 0o755); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	Browse(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `href="/?path=a%26b"`) {
		t.Errorf("folder link not URL-escaped correctly:\n%s", body)
	}
	if strings.Contains(body, `href="/?path=a&b"`) {
		t.Errorf("folder link contains a raw ampersand")
	}
}

func TestViewRejectsNonPreviewable(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.WriteFile(filepath.Join(root, "a.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/view?path=a.bin", nil)
	rec := httptest.NewRecorder()

	View(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func TestDownloadNotFound(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	req := httptest.NewRequest(http.MethodGet, "/download?path=missing.txt", nil)
	rec := httptest.NewRecorder()

	Download(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), root) {
		t.Errorf("response leaked internal path: %q", rec.Body.String())
	}
}
