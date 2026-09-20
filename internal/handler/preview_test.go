package handler

import (
	"bytes"
	"go-fileserver/internal/config"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestViewPreviewSizeLimit covers the boundary enforced by View: a previewable
// file whose size is at or below config.MaxPreviewSize is served, while a file
// one byte larger is rejected with 413. The limit itself is not changed here.
func TestViewPreviewSizeLimit(t *testing.T) {
	const limit = 8

	root := t.TempDir()
	withSharedRoot(t, root)
	config.MaxPreviewSize = limit

	tests := []struct {
		name       string
		size       int
		wantStatus int
	}{
		{"empty file", 0, http.StatusOK},
		{"well under limit", 4, http.StatusOK},
		{"exactly at limit", limit, http.StatusOK},
		{"one byte over limit", limit + 1, http.StatusRequestEntityTooLarge},
		{"far over limit", limit * 100, http.StatusRequestEntityTooLarge},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := bytes.Repeat([]byte("x"), tt.size)
			name := "case" + strconv.Itoa(i) + ".txt"
			if err := os.WriteFile(filepath.Join(root, name), content, 0o644); err != nil {
				t.Fatal(err)
			}

			req := httptest.NewRequest(http.MethodGet, "/view?path="+name, nil)
			rec := httptest.NewRecorder()

			View(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				if got := rec.Body.Len(); got != tt.size {
					t.Errorf("body length = %d, want %d", got, tt.size)
				}
				if !bytes.Equal(rec.Body.Bytes(), content) {
					t.Errorf("body does not match file content")
				}
			} else if rec.Body.Len() != 0 && bytes.Equal(rec.Body.Bytes(), content) {
				t.Errorf("rejected preview leaked the file content")
			}
		})
	}
}

// A file exactly at the limit is the last accepted size; the rejection begins
// strictly above it. This guards against an off-by-one such as >= vs >.
func TestViewPreviewSizeLimitIsExclusiveUpperBound(t *testing.T) {
	const limit = 16

	root := t.TempDir()
	withSharedRoot(t, root)
	config.MaxPreviewSize = limit

	if err := os.WriteFile(filepath.Join(root, "at.txt"), bytes.Repeat([]byte("a"), limit), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "over.txt"), bytes.Repeat([]byte("b"), limit+1), 0o644); err != nil {
		t.Fatal(err)
	}

	atRec := httptest.NewRecorder()
	View(atRec, httptest.NewRequest(http.MethodGet, "/view?path=at.txt", nil))
	if atRec.Code != http.StatusOK {
		t.Errorf("at-limit status = %d, want 200", atRec.Code)
	}

	overRec := httptest.NewRecorder()
	View(overRec, httptest.NewRequest(http.MethodGet, "/view?path=over.txt", nil))
	if overRec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("over-limit status = %d, want 413", overRec.Code)
	}
}
