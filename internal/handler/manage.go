package handler

import (
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"simple-http-fileserver-go/internal/service"
)

func Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rel := r.FormValue("path")

	if err := service.Delete(rel); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/?path="+url.QueryEscape(parentDir(rel)), http.StatusSeeOther)
}

func Rename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rel := r.FormValue("path")
	newName := r.FormValue("newname")

	if err := service.Rename(rel, newName); err != nil {
		if errors.Is(err, service.ErrFileExists) {
			http.Error(w, "A file/folder named '"+newName+"' already exists", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/?path="+url.QueryEscape(parentDir(rel)), http.StatusSeeOther)
}

func parentDir(rel string) string {
	dir := filepath.Dir(rel)
	if dir == "." {
		return ""
	}
	return dir
}
