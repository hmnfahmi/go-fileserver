package handler

import (
	"go-fileserver/internal/model"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIconForKind(t *testing.T) {
	tests := []struct {
		kind model.FileKind
		want string
	}{
		{model.KindDirectory, "📂"},
		{model.KindImage, "🖼️"},
		{model.KindVideo, "🎬"},
		{model.KindAudio, "🎵"},
		{model.KindArchive, "🗜️"},
		{model.KindDocument, "📄"},
		{model.KindCode, "📜"},
		{model.KindGeneric, "📃"},
		{model.FileKind("unknown"), "📃"},
		{model.FileKind(""), "📃"},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			if got := iconForKind(tt.kind); got != tt.want {
				t.Errorf("iconForKind(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestIsImageKind(t *testing.T) {
	if !isImageKind(model.KindImage) {
		t.Error("KindImage should be an image kind")
	}

	for _, kind := range []model.FileKind{
		model.KindDirectory, model.KindVideo, model.KindAudio,
		model.KindArchive, model.KindDocument, model.KindCode, model.KindGeneric,
	} {
		if isImageKind(kind) {
			t.Errorf("%q should not be an image kind", kind)
		}
	}
}

// The listing must render a per-kind icon and label image previews distinctly
// from text previews.
func TestBrowseRendersIconsAndImageLabel(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeBytes(t, filepath.Join(root, "photo.png"), pngMagic)
	writeBytes(t, filepath.Join(root, "notes.txt"), []byte("hello"))
	writeBytes(t, filepath.Join(root, "main.go"), []byte("package main"))
	writeBytes(t, filepath.Join(root, "archive.zip"), []byte("PK\x03\x04"))
	writeBytes(t, filepath.Join(root, "clip.mp4"), []byte("video"))
	writeBytes(t, filepath.Join(root, "song.mp3"), []byte("audio"))
	writeBytes(t, filepath.Join(root, "blob.bin"), []byte("data"))

	rec := httptest.NewRecorder()
	Browse(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("browse status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	for _, want := range []string{"📂", "🖼️", "📄", "📜", "🗜️", "🎬", "🎵", "📃"} {
		if !strings.Contains(body, want) {
			t.Errorf("listing is missing icon %q", want)
		}
	}

	// Every entry gets an icon span (1 directory + 7 files).
	if got := strings.Count(body, `class="file-icon"`); got != 8 {
		t.Errorf("icon spans = %d, want 8", got)
	}

	// The image preview is labelled "Image"; the text file keeps "Preview".
	if !strings.Contains(body, "🖼 Image") {
		t.Error("image preview label missing")
	}
	if !strings.Contains(body, "👁 Preview") {
		t.Error("text preview label missing")
	}
}

// A directory named like an image must render the folder icon, not the image
// icon, and must not offer a preview link.
func TestBrowseDirectoryNamedLikeImage(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "album.png"), 0o755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	Browse(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "📂") {
		t.Error("directory icon missing for image-named directory")
	}
	if strings.Contains(body, "🖼️") {
		t.Error("image icon rendered for a directory")
	}
	if strings.Contains(body, "/view?path=album.png") {
		t.Error("directory was offered a preview link")
	}
}
