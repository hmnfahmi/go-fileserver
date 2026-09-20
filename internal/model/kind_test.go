package model

import "testing"

func TestClassifyFileKind(t *testing.T) {
	tests := []struct {
		name  string
		isDir bool
		want  FileKind
	}{
		{"directory", true, KindDirectory},
		{"photo.png", false, KindImage},
		{"photo.PNG", false, KindImage},
		{"photo.JpEg", false, KindImage},
		{"clip.mp4", false, KindVideo},
		{"song.mp3", false, KindAudio},
		{"bundle.tar.gz", false, KindArchive},
		{"notes.txt", false, KindDocument},
		{"report.pdf", false, KindDocument},
		{"main.go", false, KindCode},
		{"index.HTML", false, KindCode},
		{"data.json", false, KindCode},
		{"noextension", false, KindGeneric},
		{"", false, KindGeneric},
		{".hidden", false, KindGeneric},
		{"archive.tar.gz", false, KindArchive},
		{"a.b.png", false, KindImage},
		// A path must never be treated as an extension: only the final
		// component's extension counts.
		{"some/dir/photo.png", false, KindImage},
		{"some/dir/noext", false, KindGeneric},
		{"weird.", false, KindGeneric},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyFileKind(tt.name, tt.isDir); got != tt.want {
				t.Errorf("ClassifyFileKind(%q, %v) = %q, want %q", tt.name, tt.isDir, got, tt.want)
			}
		})
	}
}

// A directory is always a directory, even when its name ends in a file
// extension.
func TestClassifyFileKindDirectoryWins(t *testing.T) {
	if got := ClassifyFileKind("album.png", true); got != KindDirectory {
		t.Errorf("ClassifyFileKind(album.png, dir) = %q, want %q", got, KindDirectory)
	}
}

// The classification must be case-insensitive without allocating differently.
func TestClassifyFileKindCaseInsensitive(t *testing.T) {
	for _, name := range []string{"A.PNG", "a.Png", "a.pNG", "a.png"} {
		if got := ClassifyFileKind(name, false); got != KindImage {
			t.Errorf("ClassifyFileKind(%q) = %q, want %q", name, got, KindImage)
		}
	}
}
