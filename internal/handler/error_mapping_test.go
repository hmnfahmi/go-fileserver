package handler

import (
	"errors"
	"fmt"
	"go-fileserver/internal/service"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F5/F3. Raw filesystem errors that escape the sentinel model must still map to
// accurate HTTP statuses instead of 500.
func TestStatusForErrorClassifiesOSErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, http.StatusInternalServerError},
		{"access denied sentinel", service.ErrAccessDenied, http.StatusForbidden},
		{"not found sentinel", service.ErrNotFound, http.StatusNotFound},
		{"invalid path", service.ErrInvalidPath, http.StatusBadRequest},
		{"invalid name", service.ErrInvalidName, http.StatusBadRequest},
		{"root operation", service.ErrRootOperation, http.StatusBadRequest},
		{"not directory", service.ErrNotDirectory, http.StatusBadRequest},
		{"is directory", service.ErrIsDirectory, http.StatusBadRequest},
		{"file exists", service.ErrFileExists, http.StatusConflict},
		{"raw not exist", os.ErrNotExist, http.StatusNotFound},
		{"wrapped not exist", fmt.Errorf("stat: %w", os.ErrNotExist), http.StatusNotFound},
		{"path error not exist", &os.PathError{Op: "stat", Path: "/secret", Err: os.ErrNotExist}, http.StatusNotFound},
		{"raw permission", os.ErrPermission, http.StatusForbidden},
		{"wrapped permission", fmt.Errorf("open: %w", os.ErrPermission), http.StatusForbidden},
		{"raw exist", os.ErrExist, http.StatusConflict},
		{"wrapped exist", fmt.Errorf("rename: %w", os.ErrExist), http.StatusConflict},
		{"path error exist", &os.PathError{Op: "rename", Path: "/secret", Err: os.ErrExist}, http.StatusConflict},
		{"path error unexpected", &os.PathError{Op: "write", Path: "/secret", Err: errors.New("disk failure")}, http.StatusInternalServerError},
		{"generic", errors.New("boom"), http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusForError(tc.err); got != tc.want {
				t.Errorf("statusForError(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// F5. A resource missing at handler time must be 404, not 500, for every
// endpoint. This covers the case where a file disappears between resolution and
// use, without relying on a timing-dependent race.
func TestMissingResourceReturnsNotFound(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	cases := []struct {
		name   string
		call   func(http.ResponseWriter, *http.Request)
		method string
		target string
	}{
		{"view", View, http.MethodGet, "/view?path=missing.txt"},
		{"download", Download, http.MethodGet, "/download?path=missing.txt"},
		{"delete", Delete, http.MethodPost, "/delete?path=missing.txt"},
		{"rename source", Rename, http.MethodPost, "/rename?path=missing.txt&newname=other.txt"},
		{"browse directory", Browse, http.MethodGet, "/?path=missingdir"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.call(rec, httptest.NewRequest(tc.method, tc.target, nil))

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
			if strings.Contains(rec.Body.String(), root) {
				t.Errorf("response leaked internal path: %q", rec.Body.String())
			}
		})
	}
}

// F8. Media-type classification for the upload response. Only an explicitly
// named application/json counts; wildcards and lookalikes do not.
func TestWantsJSONResponse(t *testing.T) {
	cases := []struct {
		accept string
		want   bool
	}{
		{"application/json", true},
		{"application/json; charset=utf-8", true},
		{"application/json, text/plain", true},
		{"text/plain, application/json", true},
		{"application/json;q=0.9", true},
		{"APPLICATION/JSON", true},
		{"application/json-malicious", false},
		{"application/jsonp", false},
		{"application/*", false},
		{"*/*", false},
		{"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", false},
		{"text/plain", false},
		{"", false},
	}

	for _, tc := range cases {
		t.Run(tc.accept, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/upload", nil)
			if tc.accept != "" {
				req.Header.Set("Accept", tc.accept)
			}

			if got := wantsJSONResponse(req); got != tc.want {
				t.Errorf("wantsJSONResponse(%q) = %v, want %v", tc.accept, got, tc.want)
			}
		})
	}
}

// The documented decision: a browser's normal form Accept (which ends in
// */*;q=0.8) must keep the redirect behaviour, so JSON is not selected.
func TestBrowserAcceptKeepsRedirect(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	req := multipartUploadRequest(t, "", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		map[string]string{"browser.txt": "data"})

	rec := httptest.NewRecorder()
	Upload(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 redirect", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "browser.txt")); err != nil {
		t.Fatalf("upload did not happen: %v", err)
	}
}
