package handler

import (
	"bytes"
	"go-fileserver/internal/config"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildMultipartUpload builds a multipart/form-data request body with the given
// files keyed by filename.
func buildMultipartUpload(t *testing.T, files map[string]string) (*bytes.Buffer, string) {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for name, content := range files {
		part, err := writer.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(part, content); err != nil {
			t.Fatal(err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return body, writer.FormDataContentType()
}

// multipartUploadRequest builds a POST /upload request against the given
// logical directory, using the supplied Accept header (when non-empty).
func multipartUploadRequest(t *testing.T, dirRel string, accept string, files map[string]string) *http.Request {
	t.Helper()

	body, contentType := buildMultipartUpload(t, files)

	target := "/upload"
	if dirRel != "" {
		target += "?path=" + dirRel
	}

	req := httptest.NewRequest(http.MethodPost, target, body)
	req.Header.Set("Content-Type", contentType)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	return req
}

// F6. A request body larger than MaxUploadSize must be 413, not 400.
func TestUploadOversizedReturns413(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	// withSharedRoot sets MaxUploadSize to 1 MB; send a file comfortably over it.
	big := strings.Repeat("a", int(config.MaxUploadSize)+4096)

	req := multipartUploadRequest(t, "", "application/json", map[string]string{"big.bin": big})

	rec := httptest.NewRecorder()
	Upload(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if rec.Code == http.StatusBadRequest {
		t.Fatal("oversized upload was misclassified as 400")
	}
}

// F6. A single file under the limit must still succeed. The limit applies to the
// whole request, so this stays comfortably below it.
func TestUploadUnderLimitSucceeds(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	req := multipartUploadRequest(t, "", "application/json", map[string]string{"small.txt": "hello"})

	rec := httptest.NewRecorder()
	Upload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if !strings.Contains(rec.Body.String(), `"uploaded":["small.txt"]`) {
		t.Errorf("body = %q, want uploaded small.txt", rec.Body.String())
	}

	content, err := os.ReadFile(filepath.Join(root, "small.txt"))
	if err != nil {
		t.Fatalf("uploaded file missing: %v", err)
	}
	if string(content) != "hello" {
		t.Errorf("content = %q, want %q", content, "hello")
	}
}

// F6. The limit applies to the whole request: two files that each fit but
// together exceed MaxUploadSize must still be 413.
func TestUploadMultipleFilesExceedingTotalReturns413(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	half := int(config.MaxUploadSize/2) + 2048
	files := map[string]string{
		"a.bin": strings.Repeat("a", half),
		"b.bin": strings.Repeat("b", half),
	}

	req := multipartUploadRequest(t, "", "application/json", files)

	rec := httptest.NewRecorder()
	Upload(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (total request over limit)", rec.Code)
	}
}

// F8. JSON Accept variants must all yield the JSON response.
func TestUploadJSONAcceptVariants(t *testing.T) {
	accepts := []string{
		"application/json",
		"application/json; charset=utf-8",
		"application/json, text/plain",
		"text/plain, application/json",
	}

	for _, accept := range accepts {
		t.Run(accept, func(t *testing.T) {
			root := t.TempDir()
			withSharedRoot(t, root)

			req := multipartUploadRequest(t, "", accept, map[string]string{"f.txt": "data"})

			rec := httptest.NewRecorder()
			Upload(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
		})
	}
}

// F8. A lookalike media type must not trigger the JSON branch; the browser
// redirect path is used instead.
func TestUploadJSONLookalikeIsNotJSON(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	req := multipartUploadRequest(t, "", "application/json-malicious", map[string]string{"f.txt": "data"})

	rec := httptest.NewRecorder()
	Upload(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 redirect", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct == "application/json" {
		t.Errorf("lookalike Accept produced a JSON response")
	}
}

// F6. A genuinely malformed multipart body must still be 400.
func TestUploadMalformedMultipartReturns400(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("not a multipart body"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=----does-not-matter")
	req.Header.Set("Accept", "application/json")

	rec := httptest.NewRecorder()
	Upload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// Existing behaviour: multi-upload still works and reports each file.
func TestUploadMultipleFiles(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	req := multipartUploadRequest(t, "", "application/json", map[string]string{
		"a.txt": "aaa",
		"b.txt": "bbb",
	})

	rec := httptest.NewRecorder()
	Upload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	for _, name := range []string{"a.txt", "b.txt"} {
		if !strings.Contains(body, name) {
			t.Errorf("response missing %q: %s", name, body)
		}
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("uploaded file %q missing: %v", name, err)
		}
	}
}

// Existing behaviour: a duplicate upload stays a conflict (409 for the browser
// path, and reported in the JSON "conflicts" list).
func TestUploadConflictRemainsConflict(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.WriteFile(filepath.Join(root, "dup.txt"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	// JSON path: reported as a conflict, HTTP 200 with the existing structure.
	reqJSON := multipartUploadRequest(t, "", "application/json", map[string]string{"dup.txt": "second"})
	recJSON := httptest.NewRecorder()
	Upload(recJSON, reqJSON)

	if recJSON.Code != http.StatusOK {
		t.Fatalf("json conflict status = %d, want 200", recJSON.Code)
	}
	if !strings.Contains(recJSON.Body.String(), `"conflicts":["dup.txt"]`) {
		t.Errorf("body = %q, want conflicts [dup.txt]", recJSON.Body.String())
	}
	if !strings.Contains(recJSON.Body.String(), `"uploaded":[]`) {
		t.Errorf("body = %q, want empty uploaded", recJSON.Body.String())
	}

	// Browser path: a conflict must not redirect to a success page.
	reqForm := multipartUploadRequest(t, "", "", map[string]string{"dup.txt": "second"})
	recForm := httptest.NewRecorder()
	Upload(recForm, reqForm)

	if recForm.Code != http.StatusConflict {
		t.Fatalf("form conflict status = %d, want 409", recForm.Code)
	}

	content, err := os.ReadFile(filepath.Join(root, "dup.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Errorf("existing file was overwritten: %q", content)
	}
}
