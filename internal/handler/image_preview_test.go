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

// Minimal real byte sequences so http.DetectContentType recognises each format.
var (
	pngMagic  = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 13, 'I', 'H', 'D', 'R'}
	jpegMagic = []byte{0xff, 0xd8, 0xff, 0xe0, 0, 0x10, 'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1}
	gifMagic  = []byte("GIF89a" + "\x01\x00\x01\x00\x00\x00\x00;")
	webpMagic = []byte("RIFF" + "\x1a\x00\x00\x00" + "WEBPVP8 " + "\x0e\x00\x00\x00")
)

func writeBytes(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestViewImagePreviewServesSniffedType checks that each supported image
// extension is served with the MIME type derived from the file's bytes, not
// from the extension.
func TestViewImagePreviewServesSniffedType(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		content  []byte
		wantType string
	}{
		{"png", "photo.png", pngMagic, "image/png"},
		{"jpeg", "photo.jpg", jpegMagic, "image/jpeg"},
		{"jpeg long ext", "photo.jpeg", jpegMagic, "image/jpeg"},
		{"gif", "photo.gif", gifMagic, "image/gif"},
		{"webp", "photo.webp", webpMagic, "image/webp"},
	}

	root := t.TempDir()
	withSharedRoot(t, root)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeBytes(t, filepath.Join(root, tt.file), tt.content)

			rec := httptest.NewRecorder()
			View(rec, httptest.NewRequest(http.MethodGet, "/view?path="+tt.file, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != tt.wantType {
				t.Errorf("Content-Type = %q, want %q", got, tt.wantType)
			}
			if !bytes.Equal(rec.Body.Bytes(), tt.content) {
				t.Errorf("served body does not match file content")
			}
		})
	}
}

// An image extension whose bytes are not an image must never be served as an
// image: the response is 415 and no bytes leak.
func TestViewImageExtensionWithNonImageBytesIsRejected(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "fake.png"), []byte("<?php echo 'pwned'; ?>"))

	rec := httptest.NewRecorder()
	View(rec, httptest.NewRequest(http.MethodGet, "/view?path=fake.png", nil))

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("pwned")) {
		t.Error("response leaked payload bytes")
	}
}

// A text file renamed to an image extension is still rejected, because the
// bytes are sniffed rather than trusted.
func TestViewImageExtensionWithTextBytesIsRejected(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "note.png"), []byte("just some plain text, definitely not a png"))

	rec := httptest.NewRecorder()
	View(rec, httptest.NewRequest(http.MethodGet, "/view?path=note.png", nil))

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

// SVG is excluded on purpose: it must not be classified as a previewable image
// and must be rejected, keeping same-origin script execution impossible.
func TestViewSVGIsNotPreviewable(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	writeBytes(t, filepath.Join(root, "evil.svg"), svg)

	rec := httptest.NewRecorder()
	View(rec, httptest.NewRequest(http.MethodGet, "/view?path=evil.svg", nil))

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("alert")) {
		t.Error("response leaked the svg payload")
	}
}

// Unsupported image-like formats are not previewable even with valid image
// bytes.
func TestViewUnsupportedImageFormats(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	for _, name := range []string{"image.bmp", "image.tiff", "image.tif", "image.ico", "image.avif"} {
		writeBytes(t, filepath.Join(root, name), pngMagic)

		rec := httptest.NewRecorder()
		View(rec, httptest.NewRequest(http.MethodGet, "/view?path="+name, nil))

		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("%s: status = %d, want 415", name, rec.Code)
		}
	}
}

// The image preview honours the size limit exactly like the text preview.
func TestViewImagePreviewSizeLimit(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	atLimit := append([]byte{}, pngMagic...)
	atLimit = append(atLimit, bytes.Repeat([]byte("a"), 100)...)
	overLimit := append([]byte{}, pngMagic...)
	overLimit = append(overLimit, bytes.Repeat([]byte("b"), 200)...)

	config.MaxPreviewSize = int64(len(atLimit))

	writeBytes(t, filepath.Join(root, "at.png"), atLimit)
	writeBytes(t, filepath.Join(root, "over.png"), overLimit)

	atRec := httptest.NewRecorder()
	View(atRec, httptest.NewRequest(http.MethodGet, "/view?path=at.png", nil))
	if atRec.Code != http.StatusOK {
		t.Errorf("at-limit status = %d, want 200", atRec.Code)
	}

	overRec := httptest.NewRecorder()
	View(overRec, httptest.NewRequest(http.MethodGet, "/view?path=over.png", nil))
	if overRec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("over-limit status = %d, want 413", overRec.Code)
	}
	if bytes.Contains(overRec.Body.Bytes(), []byte("b")) && bytes.Contains(overRec.Body.Bytes(), pngMagic) {
		t.Error("over-limit response leaked image bytes")
	}
}

// A directory whose name looks like an image must not be previewable.
func TestViewImageNamedDirectoryRejected(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.MkdirAll(filepath.Join(root, "album.png"), 0o755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	View(rec, httptest.NewRequest(http.MethodGet, "/view?path=album.png", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// Upper-case image extensions are previewable: classification is case-insensitive.
func TestViewImageExtensionIsCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "SHOT.PNG"), pngMagic)

	rec := httptest.NewRecorder()
	View(rec, httptest.NewRequest(http.MethodGet, "/view?path=SHOT.PNG", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
}

// A missing image must 404 rather than 415: the missing check runs first.
func TestViewMissingImageIsNotFound(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	rec := httptest.NewRecorder()
	View(rec, httptest.NewRequest(http.MethodGet, "/view?path=ghost.png", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// Traversal through the preview endpoint must be rejected before any file is
// opened, for image extensions as well as text.
func TestViewImageTraversalRejected(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	outside := filepath.Join(filepath.Dir(root), "outside.png")
	writeBytes(t, outside, pngMagic)

	for _, p := range []string{"../outside.png", "..%2Foutside.png", "sub/../../outside.png"} {
		rec := httptest.NewRecorder()
		View(rec, httptest.NewRequest(http.MethodGet, "/view?path="+p, nil))

		if rec.Code == http.StatusOK {
			t.Errorf("path %q: status = 200, want rejection", p)
		}
		if bytes.Contains(rec.Body.Bytes(), pngMagic) {
			t.Errorf("path %q: traversal leaked file bytes", p)
		}
	}
}

// A HEAD request to the image preview must not carry a body.
func TestViewImageHeadHasNoBody(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "photo.png"), pngMagic)

	rec := httptest.NewRecorder()
	View(rec, httptest.NewRequest(http.MethodHead, "/view?path=photo.png", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("HEAD body length = %d, want 0", rec.Body.Len())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
}

// The image preview must set a lock-down Content-Security-Policy so a stray
// active format cannot execute in this origin.
func TestViewImageSetsCSP(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "photo.png"), pngMagic)

	rec := httptest.NewRecorder()
	View(rec, httptest.NewRequest(http.MethodGet, "/view?path=photo.png", nil))

	if csp := rec.Header().Get("Content-Security-Policy"); csp == "" {
		t.Error("Content-Security-Policy header missing")
	}
	if cl := rec.Header().Get("Content-Length"); cl != strconv.Itoa(len(pngMagic)) {
		t.Errorf("Content-Length = %q, want %d", cl, len(pngMagic))
	}
}

// A non-image extension that happens to contain image bytes stays a text
// preview: dispatch is by extension first.
func TestViewTextFileWithImageBytesStaysText(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeBytes(t, filepath.Join(root, "data.txt"), pngMagic)

	rec := httptest.NewRecorder()
	View(rec, httptest.NewRequest(http.MethodGet, "/view?path=data.txt", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain", got)
	}
}
