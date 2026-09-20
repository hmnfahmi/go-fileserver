package handler

import (
	"encoding/json"
	"errors"
	"go-fileserver/internal/config"
	"go-fileserver/internal/service"
	"io"
	"log"
	"net/http"
	"net/url"
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
		http.Error(w, "Invalid upload request", http.StatusBadRequest)
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
			http.Error(
				w,
				"Failed reading upload (possibly too large)",
				http.StatusBadRequest,
			)
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
	wantsJSON := r.Header.Get("Accept") == "application/json"

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
