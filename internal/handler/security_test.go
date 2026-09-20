package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var browseHrefPattern = regexp.MustCompile(`href="/\?path=([^"]*)"`)

// F. Special characters in folder names must be percent-encoded exactly once so
// the browser receives a valid URL and the query stays a single parameter.
func TestBrowseURLEncodesSpecialNames(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	wantRaw := map[string]string{
		"a&b": "a%26b",
		"a#b": "a%23b",
		"a?b": "a%3fb",
		"a%b": "a%25b",
		"a+b": "a%2bb",
		"a b": "a%20b",
		"日本語": "%e6%97%a5%e6%9c%ac%e8%aa%9e",
		"a=b": "a%3db",
	}

	for name := range wantRaw {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	rec := httptest.NewRecorder()
	Browse(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("browse status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	gotRaw := map[string]string{}
	for _, match := range browseHrefPattern.FindAllStringSubmatch(body, -1) {
		raw := match[1]

		parsed, err := url.Parse("/?path=" + raw)
		if err != nil {
			t.Fatalf("href %q is not a valid URL: %v", raw, err)
		}
		if params := parsed.Query(); len(params) != 1 {
			t.Errorf("href %q produced %d query parameters, want 1", raw, len(params))
		}

		gotRaw[parsed.Query().Get("path")] = raw
	}

	for name, want := range wantRaw {
		raw, ok := gotRaw[name]
		if !ok {
			t.Errorf("folder %q did not round-trip through its link", name)
			continue
		}
		if !strings.EqualFold(raw, want) {
			t.Errorf("folder %q raw = %q, want %q", name, raw, want)
		}

		// The server must decode the rendered link back to the folder.
		rr := httptest.NewRecorder()
		Browse(rr, httptest.NewRequest(http.MethodGet, "/?path="+raw, nil))
		if rr.Code != http.StatusOK {
			t.Errorf("server did not round-trip %q (status %d)", name, rr.Code)
		}
	}
}

// F. Nested names keep the slash encoded inside the query value.
func TestBrowseURLEncodesNestedName(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.MkdirAll(filepath.Join(root, "nested", "a&b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "a&b", "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	Browse(rec, httptest.NewRequest(http.MethodGet, "/?path=nested", nil))

	var nestedRaw string
	for _, match := range browseHrefPattern.FindAllStringSubmatch(rec.Body.String(), -1) {
		parsed, err := url.Parse("/?path=" + match[1])
		if err != nil {
			continue
		}
		if parsed.Query().Get("path") == "nested/a&b" {
			nestedRaw = match[1]
		}
	}

	if nestedRaw == "" {
		t.Fatalf("nested folder link was not rendered")
	}
	if !strings.EqualFold(nestedRaw, "nested%2fa%26b") {
		t.Errorf("nested raw = %q, want %q", nestedRaw, "nested%2fa%26b")
	}

	rr := httptest.NewRecorder()
	Browse(rr, httptest.NewRequest(http.MethodGet, "/?path="+nestedRaw, nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "file.txt") {
		t.Errorf("nested round-trip failed (status %d)", rr.Code)
	}
}

// F. Links must be encoded once, never twice.
func TestBrowseLinksAreNotDoubleEncoded(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "a&b"), 0o755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	Browse(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	if strings.Contains(body, "%2526") {
		t.Errorf("output contains a double-encoded ampersand")
	}
	if !strings.Contains(body, "%26") {
		t.Errorf("output does not contain a single-encoded ampersand")
	}
}

// G. HEAD is intentionally supported on the read-only endpoints and the body is
// suppressed by the HTTP server.
func TestHeadRequestsHaveNoBody(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", Browse)
	mux.HandleFunc("/view", View)
	mux.HandleFunc("/download", Download)

	server := httptest.NewServer(mux)
	defer server.Close()

	cases := []struct {
		path string
		want int
	}{
		{"/", http.StatusOK},
		{"/view?path=f.txt", http.StatusOK},
		{"/download?path=f.txt", http.StatusOK},
	}

	for _, tc := range cases {
		resp, err := http.Head(server.URL + tc.path)
		if err != nil {
			t.Fatalf("HEAD %s: %v", tc.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != tc.want {
			t.Errorf("HEAD %s status = %d, want %d", tc.path, resp.StatusCode, tc.want)
		}
		if len(body) != 0 {
			t.Errorf("HEAD %s returned a body of %d bytes", tc.path, len(body))
		}
	}
}

// G. Disallowed methods return 405 with the correct Allow header.
func TestMethodNotAllowedAllowHeaders(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	cases := []struct {
		name   string
		call   func(http.ResponseWriter, *http.Request)
		method string
		want   string
	}{
		{"browse", Browse, http.MethodPut, "GET, HEAD"},
		{"view", View, http.MethodPost, "GET, HEAD"},
		{"download", Download, http.MethodPost, "GET, HEAD"},
		{"upload", Upload, http.MethodGet, "POST"},
		{"delete", Delete, http.MethodGet, "POST"},
		{"rename", Rename, http.MethodGet, "POST"},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, "/", nil)
		rec := httptest.NewRecorder()

		tc.call(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", tc.name, rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != tc.want {
			t.Errorf("%s: Allow = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// G. Method rejection must happen before any filesystem mutation.
func TestMethodRejectionPrecedesMutation(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	victim := filepath.Join(root, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	deleteRec := httptest.NewRecorder()
	Delete(deleteRec, httptest.NewRequest(http.MethodGet, "/delete?path=victim.txt", nil))
	if deleteRec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /delete status = %d, want 405", deleteRec.Code)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("file was deleted by a rejected GET /delete: %v", err)
	}

	renameRec := httptest.NewRecorder()
	Rename(renameRec, httptest.NewRequest(http.MethodGet, "/rename?path=victim.txt&newname=gone.txt", nil))
	if renameRec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /rename status = %d, want 405", renameRec.Code)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("source file was moved by a rejected GET /rename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "gone.txt")); !os.IsNotExist(err) {
		t.Errorf("rename happened despite a rejected GET /rename")
	}
}

// H. A traversal-looking search value is only a filename substring filter and
// is never treated as a path.
func TestBrowseSearchIsNotAPath(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.WriteFile(filepath.Join(root, "etc.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	Browse(rec, httptest.NewRequest(http.MethodGet, "/?search=../../etc", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "etc.txt") {
		t.Errorf("traversal-looking search matched a filename as a substring")
	}

	rec = httptest.NewRecorder()
	Browse(rec, httptest.NewRequest(http.MethodGet, "/?search=etc", nil))
	if !strings.Contains(rec.Body.String(), "etc.txt") {
		t.Errorf("substring search did not match the file name")
	}
}
