package handler

import (
	"errors"
	"go-fileserver/internal/config"
	"go-fileserver/internal/model"
	"go-fileserver/internal/service"
	simpleweb "go-fileserver/web"
	"html/template"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
)

// indexTemplate is parsed from the embedded template at startup.
var indexTemplate = template.Must(template.ParseFS(simpleweb.Templates, "templates/index.html"))

type BreadcrumbItem struct {
	Name string
	Path string
}

// allowMethods checks the request method and, when it is not permitted, writes
// a 405 response with the correct Allow header. It returns true when the caller
// should continue handling the request.
func allowMethods(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, method := range methods {
		if r.Method == method {
			return true
		}
	}

	w.Header().Set("Allow", strings.Join(methods, ", "))
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	return false
}

// writeServiceError logs the underlying filesystem error and writes a
// client-safe message with the appropriate HTTP status. Internal error details
// (absolute paths, OS error strings) never reach the client.
func writeServiceError(w http.ResponseWriter, err error) {
	status := statusForError(err)
	if status >= http.StatusInternalServerError {
		log.Printf("[ERROR] %v", err)
	}
	http.Error(w, service.PublicMessage(err), status)
}

func statusForError(err error) int {
	switch {
	case errors.Is(err, service.ErrAccessDenied):
		return http.StatusForbidden
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrInvalidPath),
		errors.Is(err, service.ErrInvalidName):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrRootOperation),
		errors.Is(err, service.ErrNotDirectory),
		errors.Is(err, service.ErrIsDirectory):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrFileExists):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func Browse(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
		return
	}

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
		writeServiceError(w, err)
		return
	}

	// Use the canonical logical path so links and forms built by the template
	// always contain slash-separated, traversal-free paths.
	current, err := service.CleanRel(rel)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	breadcrumb := buildBreadcrumb(current)

	data := struct {
		Current     string
		Files       any
		Search      string
		SortBy      string
		SortOrder   string
		Breadcrumbs []BreadcrumbItem
	}{
		Current:     current,
		Files:       files,
		Search:      search,
		SortBy:      sortBy,
		SortOrder:   order,
		Breadcrumbs: breadcrumb,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTemplate.Execute(w, data); err != nil {
		log.Printf("[ERROR] rendering index: %v", err)
	}
}

func Download(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
		return
	}

	rel := r.URL.Query().Get("path")

	target, err := service.ResolveExisting(rel)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	info, err := os.Stat(target)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if info.IsDir() {
		http.Error(w, "Cannot download a directory", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Disposition", `attachment; filename="`+sanitizeHeaderFilename(path.Base(target))+`"`)
	http.ServeFile(w, r, target)
}

func View(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
		return
	}

	rel := r.URL.Query().Get("path")

	target, err := service.ResolveExisting(rel)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	ext := strings.ToLower(path.Ext(target))
	if !service.PreviewableExtensions[ext] {
		http.Error(w, "File type not previewable", http.StatusUnsupportedMediaType)
		return
	}

	info, err := os.Stat(target)
	if err != nil {
		writeServiceError(w, err)
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

	data, err := os.ReadFile(target)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

// sanitizeHeaderFilename removes characters that could break the
// Content-Disposition header.
func sanitizeHeaderFilename(name string) string {
	return strings.NewReplacer(`"`, "", "\r", "", "\n", "", `\`, "").Replace(name)
}

func buildBreadcrumb(logical string) []BreadcrumbItem {
	result := []BreadcrumbItem{
		{
			Name: "Home",
			Path: "",
		},
	}

	if logical == "" {
		return result
	}

	var current string

	for _, part := range strings.Split(logical, "/") {
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
