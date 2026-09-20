package handler

import (
	"errors"
	"go-fileserver/internal/formatter"
	"go-fileserver/internal/service"
	simpleweb "go-fileserver/web"
	"html/template"
	"log"
	"math"
	"net/http"
	"net/url"
)

// editTemplate renders the server-side text editor. File content is inserted by
// html/template, which escapes it for the textarea context, so no unescaped file
// data can reach the page.
var editTemplate = template.Must(
	template.New("edit.html").ParseFS(simpleweb.Templates, "templates/edit.html"),
)

// editPageData is the view model for edit.html. Path and Version are placed in
// hidden form fields and are re-validated on every save; the server never trusts
// them.
type editPageData struct {
	Path     string
	Name     string
	Content  string
	Version  string
	SizeText string
	Parent   string
}

// Edit serves the browser editor: GET (and HEAD) render the file, POST saves it.
// It is the only endpoint that writes file content, so the service layer
// re-validates the path, the file type, the size and the version on every call.
func Edit(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodGet, http.MethodHead, http.MethodPost) {
		return
	}

	if r.Method == http.MethodPost {
		saveEdit(w, r)
		return
	}

	showEdit(w, r)
}

// showEdit renders the editor for an existing editable file.
func showEdit(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")

	doc, err := service.ReadEditableFile(rel)
	if err != nil {
		writeEditError(w, err, rel)
		return
	}

	data := editPageData{
		Path:     doc.Path,
		Name:     doc.Name,
		Content:  doc.Content,
		Version:  doc.Version.String(),
		SizeText: formatter.FormatSize(doc.Size),
		Parent:   parentDir(doc.Path),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := editTemplate.Execute(w, data); err != nil {
		log.Printf("[ERROR] rendering editor: %v", err)
	}
}

// saveEdit writes edited content back to an existing file.
//
// The request body is bounded before it is parsed, so an oversized submission is
// rejected without buffering it. The path and the version are then re-validated
// by the service: a hidden field is treated as untrusted input, never as proof
// that the file is unchanged or that the path is allowed.
func saveEdit(w http.ResponseWriter, r *http.Request) {
	limit := service.EditSizeLimit()
	r.Body = http.MaxBytesReader(w, r.Body, editBodyLimit(limit))

	if err := r.ParseForm(); err != nil {
		if isRequestTooLarge(err) {
			writeEditError(w, service.ErrTooLarge, "")
			return
		}
		renderErrorPage(w, http.StatusBadRequest, "Invalid form submission")
		return
	}

	rel := r.FormValue("path")

	version, ok := service.ParseFileVersion(r.FormValue("version"))
	if !ok {
		renderErrorPage(w, http.StatusBadRequest, "Invalid or missing file version")
		return
	}

	content := []byte(r.FormValue("content"))

	if err := service.SaveEditableFile(rel, content, version); err != nil {
		writeEditError(w, err, rel)
		return
	}

	// Redirect back to the folder that holds the file, so the updated content is
	// immediately visible in the listing and the stale editor page is left.
	http.Redirect(w, r, "/?path="+url.QueryEscape(parentDir(rel)), http.StatusSeeOther)
}

// writeEditError renders an editor failure. A conflict gets an explicit message
// and a link back to the folder, because silently overwriting the other change
// is exactly what must not happen. Every other error reuses the central mapping,
// so a missing file is 404, an unsupported type is 415 and an escaping path is
// 403.
func writeEditError(w http.ResponseWriter, err error, rel string) {
	status := statusForError(err)
	if status >= http.StatusInternalServerError {
		log.Printf("[ERROR] %v", err)
	}

	if errors.Is(err, service.ErrConflict) {
		renderErrorPageWithBack(
			w,
			http.StatusConflict,
			"File changed since you opened it. Your changes were not saved.",
			editBackHref(rel),
			"Back to folder",
		)
		return
	}

	renderErrorPageWithBack(w, status, service.PublicMessage(err), editBackHref(rel), "Back to folder")
}

// editBackHref returns a link to the folder containing the edited file. The path
// is canonicalised by parentDir and percent-encoded, so a directory name with
// spaces or other URL-special characters round-trips correctly.
func editBackHref(rel string) string {
	return "/?path=" + url.QueryEscape(parentDir(rel))
}

// editBodyLimit bounds the raw POST body. Form encoding can expand a byte to
// three (%XX), so the bound is three times the content limit plus a little room
// for the path and version fields. This is a denial-of-service guard only; the
// service still enforces the real content limit after decoding.
func editBodyLimit(limit int64) int64 {
	const overhead = 64 * 1024

	if limit > (math.MaxInt64-overhead)/3 {
		return math.MaxInt64
	}

	return limit*3 + overhead
}
