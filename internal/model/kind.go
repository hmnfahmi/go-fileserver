package model

import (
	"path/filepath"
)

// FileKind is a presentation-only classification of a directory entry used to
// choose an icon in the listing. It never influences filesystem access: every
// path still goes through the CleanRel/Resolve* security boundary.
type FileKind string

const (
	KindDirectory FileKind = "directory"
	KindImage     FileKind = "image"
	KindVideo     FileKind = "video"
	KindAudio     FileKind = "audio"
	KindArchive   FileKind = "archive"
	KindDocument  FileKind = "document"
	KindCode      FileKind = "code"
	KindGeneric   FileKind = "generic"
)

var kindByExtension = map[string]FileKind{
	// image — a superset of the formats that can be previewed inline; the
	// handler still sniffs the bytes before serving an image preview.
	".jpg": KindImage, ".jpeg": KindImage, ".png": KindImage, ".gif": KindImage,
	".webp": KindImage, ".bmp": KindImage, ".svg": KindImage, ".ico": KindImage,
	".tif": KindImage, ".tiff": KindImage,

	// video
	".mp4": KindVideo, ".mkv": KindVideo, ".mov": KindVideo, ".avi": KindVideo,
	".webm": KindVideo, ".m4v": KindVideo,

	// audio
	".mp3": KindAudio, ".wav": KindAudio, ".flac": KindAudio, ".ogg": KindAudio,
	".m4a": KindAudio, ".aac": KindAudio,

	// archive
	".zip": KindArchive, ".rar": KindArchive, ".7z": KindArchive, ".tar": KindArchive,
	".gz": KindArchive, ".bz2": KindArchive, ".xz": KindArchive, ".tgz": KindArchive,

	// document / text
	".txt": KindDocument, ".log": KindDocument, ".md": KindDocument, ".pdf": KindDocument,
	".doc": KindDocument, ".docx": KindDocument, ".xls": KindDocument, ".xlsx": KindDocument,
	".ppt": KindDocument, ".pptx": KindDocument, ".csv": KindDocument, ".rtf": KindDocument,

	// code / source
	".go": KindCode, ".js": KindCode, ".ts": KindCode, ".jsx": KindCode, ".tsx": KindCode,
	".py": KindCode, ".rb": KindCode, ".java": KindCode, ".c": KindCode, ".h": KindCode,
	".cpp": KindCode, ".cs": KindCode, ".php": KindCode, ".rs": KindCode, ".sh": KindCode,
	".html": KindCode, ".css": KindCode, ".json": KindCode, ".yaml": KindCode,
	".yml": KindCode, ".xml": KindCode, ".conf": KindCode, ".toml": KindCode, ".sql": KindCode,
}

// ClassifyFileKind returns the presentation kind for a directory entry. It is
// case-insensitive, uses only the final extension (so "archive.tar.gz" and
// "a.b.png" behave predictably), and never inspects a path: callers pass a bare
// entry name from os.ReadDir.
func ClassifyFileKind(name string, isDir bool) FileKind {
	if isDir {
		return KindDirectory
	}

	ext := lowerExt(filepath.Ext(name))
	if kind, ok := kindByExtension[ext]; ok {
		return kind
	}

	return KindGeneric
}

// lowerExt lower-cases an ASCII extension in place so the classification stays
// allocation-light and case-insensitive without pulling in the strings package.
func lowerExt(ext string) string {
	var buf [16]byte
	if len(ext) > len(buf) {
		return ext
	}

	n := copy(buf[:], ext)
	for i := 0; i < n; i++ {
		if buf[i] >= 'A' && buf[i] <= 'Z' {
			buf[i] += 'a' - 'A'
		}
	}

	return string(buf[:n])
}
