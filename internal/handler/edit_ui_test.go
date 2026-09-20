package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Editable files must offer an Edit action in the listing.
func TestBrowseRendersEditActionForEditableFiles(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	for _, name := range []string{"notes.txt", "readme.md", "data.json", "index.html"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	_, body := browseBody(t, "/")

	for _, name := range []string{"notes.txt", "readme.md", "data.json", "index.html"} {
		want := `href="/edit?path=` + name + `"`
		if !strings.Contains(body, want) {
			t.Errorf("editable file %s does not offer an Edit action", name)
		}
	}
}

// Unsupported file types must not offer an Edit action.
func TestBrowseOmitsEditActionForUnsupportedFiles(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	for _, name := range []string{"photo.png", "archive.zip", "app.exe", "binary.bin"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	_, body := browseBody(t, "/")

	for _, name := range []string{"photo.png", "archive.zip", "app.exe", "binary.bin"} {
		unwanted := `/edit?path=` + name
		if strings.Contains(body, unwanted) {
			t.Errorf("unsupported file %s wrongly offers an Edit action", name)
		}
	}
}

// Directories must never offer an Edit action.
func TestBrowseOmitsEditActionForDirectories(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "notes.txt"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, body := browseBody(t, "/")

	if strings.Contains(body, `href="/edit?path=notes.txt"`) {
		t.Errorf("directory named notes.txt wrongly offers an Edit action")
	}
}

// A file larger than the edit limit must not offer the action when the size is
// already known from the listing.
func TestBrowseOmitsEditActionForOversizedFiles(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)
	withEditLimit(t, 4)

	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, body := browseBody(t, "/")

	if strings.Contains(body, `href="/edit?path=big.txt"`) {
		t.Errorf("oversized file wrongly offers an Edit action")
	}
}

// The editor page must load its own stylesheet and script and expose the save
// and cancel controls.
func TestEditPageRendersEditorAssetsAndControls(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("# Notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	Edit(rec, httptest.NewRequest(http.MethodGet, "/edit?path=notes.md", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	for _, want := range []string{
		`/static/css/edit.css`,
		`/static/js/edit.js`,
		`id="edit-form"`,
		`action="/edit"`,
		`method="POST"`,
		`id="edit-content"`,
		`name="content"`,
		`id="edit-save"`,
		`href="/?path="`,
		"Edit: notes.md",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("editor page missing %q", want)
		}
	}
}
