package handler

import (
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"simple-http-fileserver-go/internal/config"
	"simple-http-fileserver-go/internal/model"
	"simple-http-fileserver-go/internal/service"
	"strings"
)

type BreadcrumbItem struct {
	Name string
	Path string
}

func Browse(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	search := r.URL.Query().Get("search")
	sortBy := r.URL.Query().Get("sortBy")
	order := r.URL.Query().Get("order")

	if sortBy == "" {
		sortBy = "name"
	}
	if order == "" {
		order = "asc"
	}

	files, err := service.List(model.ListOptions{
		Path:      rel,
		Search:    search,
		SortBy:    model.SortBy(sortBy),
		SortOrder: model.SortOrder(order),
	})

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	breadcrumb := buildBreadcrumb(rel)

	data := struct {
		Current     string
		Files       any
		Search      string
		SortBy      string
		SortOrder   string
		Breadcrumbs []BreadcrumbItem
	}{
		Current:     rel,
		Files:       files,
		Search:      search,
		SortBy:      sortBy,
		SortOrder:   order,
		Breadcrumbs: breadcrumb,
	}

	tmpl := template.Must(template.ParseFiles("web/templates/index.html"))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func Download(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	path, err := service.SafePath(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "Cannot download a directory", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(path)+`"`)
	http.ServeFile(w, r, path)
}

func View(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	path, err := service.SafePath(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	ext := strings.ToLower(filepath.Ext(path))
	if !service.PreviewableExtensions[ext] {
		http.Error(w, "File type not previewable", http.StatusUnsupportedMediaType)
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "Cannot preview a directory", http.StatusBadRequest)
		return
	}
	if info.Size() > config.MaxPreviewSize {
		http.Error(w, "File too large to preview", http.StatusRequestEntityTooLarge)
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

func buildBreadcrumb(path string) []BreadcrumbItem {
	if path == "" {
		return []BreadcrumbItem{
			{
				Name: "Home",
				Path: "",
			},
		}
	}

	var result []BreadcrumbItem

	result = append(result, BreadcrumbItem{
		Name: "Home",
		Path: "",
	})

	var current string

	for _, part := range strings.Split(path, "/") {
		if part == "" {
			continue
		}

		if current == "" {
			current = part
		} else {
			current += "/" + part
		}

		result = append(result, BreadcrumbItem{
			Name: part,
			Path: current,
		})
	}

	return result
}
