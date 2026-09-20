package handler

import (
	"errors"
	"go-fileserver/internal/service"
	"net/http"
	"net/url"
)

// CreateFolder handles POST /mkdir: it creates one new directory inside the
// current directory. The current directory and the name both travel in the form
// body; the service validates them through the shared security boundary, so the
// handler never touches filesystem paths itself.
func CreateFolder(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodPost) {
		return
	}

	dir := r.FormValue("path")
	name := r.FormValue("name")

	if err := service.CreateDirectory(dir, name); err != nil {
		writeCreateError(w, err)
		return
	}

	redirectToCurrentDir(w, r, dir)
}

// CreateFile handles POST /create-file: it creates one new, empty file inside
// the current directory. Creation is exclusive in the service layer, so an
// existing entry is reported as a conflict rather than overwritten.
func CreateFile(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodPost) {
		return
	}

	dir := r.FormValue("path")
	name := r.FormValue("name")

	if err := service.CreateEmptyFile(dir, name); err != nil {
		writeCreateError(w, err)
		return
	}

	redirectToCurrentDir(w, r, dir)
}

// writeCreateError maps a creation failure onto an HTTP status. A conflict gets
// the same explicit 409 message the rename flow uses; everything else reuses the
// central mapping so a missing parent is 404, an invalid name is 400 and an
// escaping path is 403. Raw filesystem errors are never written.
func writeCreateError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrFileExists) {
		http.Error(w, "File or folder already exists", http.StatusConflict)
		return
	}

	writeServiceError(w, err)
}

// redirectToCurrentDir sends the browser back to the directory the item was
// created in, using the project's existing 303 + query-string convention. The
// path is re-canonicalised and percent-encoded so a directory name containing
// spaces, "&", "#", "%", "+" or non-ASCII characters round-trips correctly.
func redirectToCurrentDir(w http.ResponseWriter, r *http.Request, dir string) {
	cleaned, err := service.CleanRel(dir)
	if err != nil {
		cleaned = ""
	}

	http.Redirect(w, r, "/?path="+url.QueryEscape(cleaned), http.StatusSeeOther)
}
