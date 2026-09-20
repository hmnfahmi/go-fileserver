package service

// PreviewKind describes how a file can be previewed. It is a presentation hint
// derived from the extension only; the handler still verifies the actual bytes
// before serving an image.
type PreviewKind int

const (
	// PreviewNone means the file type has no preview.
	PreviewNone PreviewKind = iota
	// PreviewText means the file is rendered as plain text.
	PreviewText
	// PreviewImage means the file is served as an image.
	PreviewImage
)

// PreviewableExtensions lists the extensions rendered as plain text. The name is
// kept for backwards compatibility; new code should use ClassifyPreview.
var PreviewableExtensions = map[string]bool{
	".txt":  true,
	".log":  true,
	".json": true,
	".yaml": true,
	".yml":  true,
	".csv":  true,
	".xml":  true,
	".conf": true,
}

// ImagePreviewExtensions lists the raster image extensions that may be previewed
// inline. SVG is deliberately excluded: it can carry scripts and would be an
// XSS vector when served from this origin. The extension only selects a
// candidate; the handler sniffs the bytes and refuses anything that is not one
// of these formats.
var ImagePreviewExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
}

// ClassifyPreview reports how the given extension (lower-case, including the
// leading dot) should be previewed. Unknown extensions return PreviewNone.
func ClassifyPreview(ext string) PreviewKind {
	switch {
	case PreviewableExtensions[ext]:
		return PreviewText
	case ImagePreviewExtensions[ext]:
		return PreviewImage
	default:
		return PreviewNone
	}
}
