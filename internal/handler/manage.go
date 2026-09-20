package handler

import (
	"errors"
	"go-fileserver/internal/service"
	"net/http"
	"net/url"
	"path"
)

func Delete(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodPost) {
		return
	}

	rel := r.FormValue("path")

	if err := service.Delete(rel); err != nil {
		writeServiceError(w, err)
		return
	}

	http.Redirect(w, r, "/?path="+url.QueryEscape(parentDir(rel)), http.StatusSeeOther)
}

func Rename(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodPost) {
		return
	}

	rel := r.FormValue("path")
	newName := r.FormValue("newname")

	if err := service.Rename(rel, newName); err != nil {
		if errors.Is(err, service.ErrFileExists) {
			http.Error(w, "A file or folder with that name already exists", http.StatusConflict)
			return
		}
		writeServiceError(w, err)
		return
	}

	http.Redirect(w, r, "/?path="+url.QueryEscape(parentDir(rel)), http.StatusSeeOther)
}

// parentDir returns the slash-separated parent of a logical path ("" at the
// root), so redirects stay in the same path style as the rest of the app.
func parentDir(rel string) string {
	cleaned, err := service.CleanRel(rel)
	if err != nil {
		return ""
	}

	dir := path.Dir(cleaned)
	if dir == "." || dir == "/" {
		return ""
	}

	return dir
}
