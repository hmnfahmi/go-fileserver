package service

import (
	"go-fileserver/internal/model"
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyPreview(t *testing.T) {
	tests := []struct {
		ext  string
		want PreviewKind
	}{
		{".txt", PreviewText},
		{".log", PreviewText},
		{".json", PreviewText},
		{".yaml", PreviewText},
		{".yml", PreviewText},
		{".csv", PreviewText},
		{".xml", PreviewText},
		{".conf", PreviewText},

		{".jpg", PreviewImage},
		{".jpeg", PreviewImage},
		{".png", PreviewImage},
		{".gif", PreviewImage},
		{".webp", PreviewImage},

		// SVG is intentionally excluded: it can carry scripts and would be an
		// XSS vector when served from this origin.
		{".svg", PreviewNone},
		// Unsupported image-like formats stay unsupported.
		{".bmp", PreviewNone},
		{".tiff", PreviewNone},
		{".avif", PreviewNone},

		{".bin", PreviewNone},
		{".exe", PreviewNone},
		{".mp4", PreviewNone},
		{"", PreviewNone},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			if got := ClassifyPreview(tt.ext); got != tt.want {
				t.Errorf("ClassifyPreview(%q) = %v, want %v", tt.ext, got, tt.want)
			}
		})
	}
}

// List must mark images previewable so the template shows the preview action,
// while still marking text files previewable and binaries not previewable.
func TestListMarksImagesPreviewable(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	for _, name := range []string{"photo.png", "shot.JPG", "anim.gif", "pic.webp", "vector.svg", "notes.txt", "blob.bin"} {
		writeFile(t, filepath.Join(root, name), "data")
	}

	items, err := List(model.ListOptions{Path: ""})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	previewable := map[string]bool{}
	for _, item := range items {
		previewable[item.Name] = item.Previewable
	}

	want := map[string]bool{
		"photo.png":  true,
		"shot.JPG":   true,
		"anim.gif":   true,
		"pic.webp":   true,
		"vector.svg": false,
		"notes.txt":  true,
		"blob.bin":   false,
	}

	for name, wantPreview := range want {
		if got := previewable[name]; got != wantPreview {
			t.Errorf("%s Previewable = %v, want %v", name, got, wantPreview)
		}
	}
}

// A directory whose name ends in an image extension must never be previewable.
func TestListDoesNotMarkDirectoriesPreviewable(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.MkdirAll(filepath.Join(root, "album.png"), 0o755); err != nil {
		t.Fatal(err)
	}

	items, err := List(model.ListOptions{Path: ""})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, item := range items {
		if item.Name == "album.png" {
			if item.Previewable {
				t.Error("directory with an image extension was marked previewable")
			}
			if item.Kind != model.KindDirectory {
				t.Errorf("Kind = %q, want %q", item.Kind, model.KindDirectory)
			}
			return
		}
	}

	t.Fatal("album.png not found in listing")
}

// List must populate the presentation Kind for every entry so the template can
// choose an icon.
func TestListPopulatesKind(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"photo.png", "clip.mp4", "song.mp3", "archive.zip", "notes.txt", "main.go", "blob.bin"} {
		writeFile(t, filepath.Join(root, name), "data")
	}

	items, err := List(model.ListOptions{Path: ""})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	want := map[string]model.FileKind{
		"folder":      model.KindDirectory,
		"photo.png":   model.KindImage,
		"clip.mp4":    model.KindVideo,
		"song.mp3":    model.KindAudio,
		"archive.zip": model.KindArchive,
		"notes.txt":   model.KindDocument,
		"main.go":     model.KindCode,
		"blob.bin":    model.KindGeneric,
	}

	for _, item := range items {
		if wantKind, ok := want[item.Name]; ok {
			if item.Kind != wantKind {
				t.Errorf("%s Kind = %q, want %q", item.Name, item.Kind, wantKind)
			}
			delete(want, item.Name)
		}
	}

	for name := range want {
		t.Errorf("entry %q missing from listing", name)
	}
}
