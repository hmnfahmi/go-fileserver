package handler

import "go-fileserver/internal/model"

// iconForKind maps a presentation file kind to the emoji shown in the listing.
// Directories keep their existing folder icon; every other kind gets a distinct
// glyph so the list is scannable at a glance. This is a pure presentation
// helper and never touches the filesystem.
func iconForKind(kind model.FileKind) string {
	switch kind {
	case model.KindDirectory:
		return "📂"
	case model.KindImage:
		return "🖼️"
	case model.KindVideo:
		return "🎬"
	case model.KindAudio:
		return "🎵"
	case model.KindArchive:
		return "🗜️"
	case model.KindDocument:
		return "📄"
	case model.KindCode:
		return "📜"
	default:
		return "📃"
	}
}

// isImageKind reports whether a file is presented as an image, so the preview
// action can be labelled accordingly.
func isImageKind(kind model.FileKind) bool {
	return kind == model.KindImage
}
