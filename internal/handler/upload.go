package handler

import (
	"encoding/json"
	"errors"
	"go-fileserver/internal/config"
	"go-fileserver/internal/service"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

type uploadResponse struct {
	Uploaded  []string          `json:"uploaded"`
	Conflicts []string          `json:"conflicts"`
	Failed    []uploadFileError `json:"failed"`
}

type uploadFileError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

func Upload(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodPost) {
		return
	}

	dirRel := r.URL.Query().Get("path")

	log.Printf("[UPLOAD] incoming request, path=%q", dirRel)

	r.Body = http.MaxBytesReader(w, r.Body, config.MaxUploadSize)

	mr, err := r.MultipartReader()
	if err != nil {
		writeUploadBodyError(w, err)
		return
	}

	result := uploadResponse{
		Uploaded:  make([]string, 0),
		Conflicts: make([]string, 0),
		Failed:    make([]uploadFileError, 0),
	}

	for {
		part, err := mr.NextPart()

		if err == io.EOF {
			break
		}

		if err != nil {
			writeUploadBodyError(w, err)
			return
		}

		if part.FormName() != "file" || part.FileName() == "" {
			part.Close()
			continue
		}

		fileName := part.FileName()

		log.Printf(
			"[UPLOAD] saving %q into %q",
			fileName,
			dirRel,
		)

		saveErr := service.SaveUpload(
			dirRel,
			fileName,
			part,
		)

		part.Close()

		log.Printf(
			"[UPLOAD] saveUpload result for %q: err=%v",
			fileName,
			saveErr,
		)

		if saveErr == nil {
			result.Uploaded = append(
				result.Uploaded,
				fileName,
			)

			continue
		}

		// A file that pushes the whole request past MaxUploadSize fails here
		// while its body is being copied. Report the request as too large
		// rather than as a per-file failure.
		if isRequestTooLarge(saveErr) {
			writeUploadBodyError(w, saveErr)
			return
		}

		if errors.Is(saveErr, service.ErrFileExists) {
			result.Conflicts = append(
				result.Conflicts,
				fileName,
			)

			continue
		}

		result.Failed = append(
			result.Failed,
			uploadFileError{
				Name:    fileName,
				Message: service.PublicMessage(saveErr),
			},
		)
	}

	if len(result.Uploaded) == 0 && len(result.Conflicts) == 0 && len(result.Failed) == 0 {
		http.Error(
			w,
			"No file uploaded",
			http.StatusBadRequest,
		)

		return
	}

	log.Printf(
		"[UPLOAD] done, uploaded=%d conflicts=%d failed=%d path=%q",
		len(result.Uploaded),
		len(result.Conflicts),
		len(result.Failed),
		dirRel,
	)

	writeUploadResponse(
		w,
		r,
		dirRel,
		result,
	)
}

func writeUploadResponse(w http.ResponseWriter, r *http.Request, dirRel string, result uploadResponse) {
	wantsJSON := wantsJSONResponse(r)

	if !wantsJSON {
		if len(result.Conflicts) > 0 || len(result.Failed) > 0 {
			http.Error(
				w,
				"Some files could not be uploaded",
				http.StatusConflict,
			)
			return
		}

		http.Redirect(
			w,
			r,
			"/?path="+url.QueryEscape(dirRel),
			http.StatusSeeOther,
		)

		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("[UPLOAD] failed writing response: %v", err)
	}
}

// isRequestTooLarge reports whether err is the *http.MaxBytesError produced when
// a request body exceeds the limit installed by http.MaxBytesReader.
func isRequestTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

// writeUploadBodyError distinguishes an oversized request body from a malformed
// multipart request. MaxUploadSize limits the whole request, so the
// *http.MaxBytesError is reported as 413 rather than a generic 400.
func writeUploadBodyError(w http.ResponseWriter, err error) {
	if isRequestTooLarge(err) {
		http.Error(w, "Upload too large", http.StatusRequestEntityTooLarge)
		return
	}

	http.Error(w, "Invalid upload request", http.StatusBadRequest)
}

// wantsJSONResponse reports whether the client explicitly accepts a JSON
// response. The Accept header is parsed as a comma-separated list of media types
// so normal client forms such as "application/json; charset=utf-8" or
// "application/json, text/plain" are recognised, while lookalikes such as
// "application/json-malicious" are not.
//
// Wildcards ("*/*", "application/*") are deliberately NOT treated as JSON: a
// browser submitting the upload form normally sends
// "text/html,...,*/*;q=0.8", and that fallback must keep the redirect
// behaviour. JSON is returned only when application/json is named explicitly.
func wantsJSONResponse(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if accept == "" {
		return false
	}

	for _, part := range strings.Split(accept, ",") {
		mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err != nil {
			continue
		}

		if mediaType == "application/json" {
			return true
		}
	}

	return false
}
