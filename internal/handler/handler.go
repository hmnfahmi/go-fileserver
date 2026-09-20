package handler

import (
	"context"
	"errors"
	"go-fileserver/internal/config"
	"go-fileserver/internal/model"
	"go-fileserver/internal/service"
	simpleweb "go-fileserver/web"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
)

// indexTemplate is parsed from the embedded template at startup. The func map
// supplies presentation-only helpers; none of them can influence path handling.
var indexTemplate = template.Must(
	template.New("index.html").Funcs(template.FuncMap{
		"icon":    iconForKind,
		"isImage": isImageKind,
	}).ParseFS(simpleweb.Templates, "templates/index.html"),
)

// errorTemplate renders a small, self-contained error page for browser
// navigations. It only ever receives a status code and an already-sanitised
// client-safe message; filesystem paths and raw errors are never passed in.
var errorTemplate = template.Must(
	template.New("error.html").ParseFS(simpleweb.Templates, "templates/error.html"),
)

// errorPageData is the view model for error.html. BackHref/BackLabel are
// optional: when set, the page offers a second navigation action (for example
// back to the folder that holds a file whose save conflicted).
type errorPageData struct {
	Status    int
	Title     string
	Message   string
	BackHref  string
	BackLabel string
}

// errorTitle maps an HTTP status to a short, human-readable heading. It never
// echoes the underlying error, so no internal detail can reach the page.
func errorTitle(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "Bad request"
	case http.StatusForbidden:
		return "Access denied"
	case http.StatusNotFound:
		return "Page not found"
	case http.StatusMethodNotAllowed:
		return "Method not allowed"
	case http.StatusConflict:
		return "Conflict"
	case http.StatusRequestEntityTooLarge:
		return "Content too large"
	case http.StatusUnsupportedMediaType:
		return "Unsupported file type"
	default:
		return "Something went wrong"
	}
}

// renderErrorPage writes an HTML error page while preserving the supplied HTTP
// status. The status is committed before the body so a 403/404 can never be
// turned into a 200 by the rendering step.
func renderErrorPage(w http.ResponseWriter, status int, message string) {
	renderErrorPageWithBack(w, status, message, "", "")
}

// renderErrorPageWithBack renders the shared error page with an optional second
// navigation action. The BackHref is built by the caller from an already
// canonicalised, client-safe path; no filesystem path is ever passed in.
func renderErrorPageWithBack(w http.ResponseWriter, status int, message, backHref, backLabel string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	data := errorPageData{
		Status:    status,
		Title:     errorTitle(status),
		Message:   message,
		BackHref:  backHref,
		BackLabel: backLabel,
	}

	if err := errorTemplate.Execute(w, data); err != nil {
		log.Printf("[ERROR] rendering error page: %v", err)
	}
}

// writeHTMLServiceError renders the shared error page for a browser-navigation
// request (browse or preview). Content endpoints such as download and zip keep
// using writeServiceError, which returns a plain-text body appropriate for a
// file transfer.
func writeHTMLServiceError(w http.ResponseWriter, err error) {
	status := statusForError(err)
	if status >= http.StatusInternalServerError {
		log.Printf("[ERROR] %v", err)
	}
	renderErrorPage(w, status, service.PublicMessage(err))
}

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
	// Filesystem errors can still surface raw when a file disappears or becomes
	// unreadable between ResolveExisting and a later operation. Classify them
	// centrally so a concurrent delete becomes 404 rather than 500.
	case errors.Is(err, os.ErrPermission):
		return http.StatusForbidden
	case errors.Is(err, os.ErrNotExist):
		return http.StatusNotFound
	case errors.Is(err, os.ErrExist):
		return http.StatusConflict
	case errors.Is(err, service.ErrInvalidPath),
		errors.Is(err, service.ErrInvalidName):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrRootOperation),
		errors.Is(err, service.ErrNotDirectory),
		errors.Is(err, service.ErrIsDirectory):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrFileExists):
		return http.StatusConflict
	case errors.Is(err, service.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, service.ErrTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, service.ErrNotEditable),
		errors.Is(err, service.ErrInvalidEncoding):
		return http.StatusUnsupportedMediaType
	case errors.Is(err, service.ErrNotRegular):
		return http.StatusBadRequest
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
		Context:   r.Context(),
	})

	if err != nil {
		// A cancelled request means the client is gone; there is no one to
		// render an error page for. A large recursive search stops at the
		// context check inside the walker.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		writeHTMLServiceError(w, err)
		return
	}

	// Use the canonical logical path so links and forms built by the template
	// always contain slash-separated, traversal-free paths.
	current, err := service.CleanRel(rel)
	if err != nil {
		writeHTMLServiceError(w, err)
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

// Zip streams a directory (or a single regular file) as a ZIP archive. The
// archive layout and the symlink policy live in the service layer; this handler
// only validates the request, sets safe headers and streams the result.
//
// The entry list is built before streaming, so missing, escaping and
// non-archivable targets are reported with a proper status. A failure that
// happens after streaming has begun cannot change the status code; the response
// is truncated rather than replaced with a fake success and the error is logged.
func Zip(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
		return
	}

	entries, download, err := service.PrepareZip(r.URL.Query().Get("path"))
	if err != nil {
		writeServiceError(w, err)
		return
	}

	filename := sanitizeHeaderFilename(download + ".zip")

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)

	// Content-Length is intentionally omitted: the archive is streamed and its
	// final size is unknown without buffering the whole thing.
	if r.Method == http.MethodHead {
		return
	}

	streamZip(w, entries)
}

// zipStreamWriter counts the bytes written to the response so Zip can tell
// whether the status line has already been sent.
type zipStreamWriter struct {
	w io.Writer
	n int64
}

func (s *zipStreamWriter) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	s.n += int64(n)
	return n, err
}

// streamZip writes the archive and separates the two failure modes:
//
//   - If nothing has been written yet, the response is still uncommitted, so a
//     clean service error (for example a file that vanished after the entry
//     list was built) is reported with the usual status and message.
//   - Once any byte has been written the status is already 200 and cannot be
//     changed. The archive is left truncated rather than completed, so the
//     client's extractor fails instead of receiving a plausible-looking but
//     incomplete archive. This limitation is inherent to streaming without
//     buffering the whole archive.
func streamZip(w http.ResponseWriter, entries []service.ZipEntry) {
	stream := &zipStreamWriter{w: w}

	if err := service.WriteZip(stream, entries); err != nil {
		if stream.n == 0 {
			w.Header().Del("Content-Type")
			w.Header().Del("Content-Disposition")
			writeServiceError(w, err)
			return
		}

		log.Printf("[ERROR] zip archive failed after streaming started: %v", err)
	}
}

func View(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodGet, http.MethodHead) {
		return
	}

	rel := r.URL.Query().Get("path")

	target, err := service.ResolveExisting(rel)
	if err != nil {
		writeHTMLServiceError(w, err)
		return
	}

	kind := service.ClassifyPreview(strings.ToLower(path.Ext(target)))
	if kind == service.PreviewNone {
		renderErrorPage(w, http.StatusUnsupportedMediaType, "File type not previewable")
		return
	}

	info, err := os.Stat(target)
	if err != nil {
		writeHTMLServiceError(w, err)
		return
	}
	if info.IsDir() {
		renderErrorPage(w, http.StatusBadRequest, "Cannot preview a directory")
		return
	}
	if info.Size() > config.MaxPreviewSize {
		renderErrorPage(w, http.StatusRequestEntityTooLarge, "File too large to preview")
		return
	}

	if kind == service.PreviewImage {
		previewImage(w, r, target, info)
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

// previewImage streams an image preview. The extension only selected the
// candidate file: the actual bytes are sniffed and anything that is not an image
// is rejected, so a renamed file cannot change the response type. http.ServeContent
// handles HEAD, range requests and Content-Length without buffering the file.
func previewImage(w http.ResponseWriter, r *http.Request, target string, info os.FileInfo) {
	f, err := os.Open(target)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	defer f.Close()

	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		writeServiceError(w, err)
		return
	}
	head = head[:n]

	contentType := http.DetectContentType(head)
	if !isImageContentType(contentType) {
		http.Error(w, "File type not previewable", http.StatusUnsupportedMediaType)
		return
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		writeServiceError(w, err)
		return
	}

	// Serve inline so the browser renders it instead of downloading.
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; sandbox")

	http.ServeContent(w, r, path.Base(target), info.ModTime(), f)
}

// isImageContentType reports whether a sniffed MIME type is a raster image that
// browsers can render inline. SVG is never returned by DetectContentType, and is
// excluded on purpose, so no SVG can be previewed as an image.
func isImageContentType(contentType string) bool {
	if semi := strings.IndexByte(contentType, ';'); semi >= 0 {
		contentType = strings.TrimSpace(contentType[:semi])
	}

	switch contentType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
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
