package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The New action and both create forms must be present on the browse page.
func TestBrowseRendersCreateActions(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	rec, body := browseBody(t, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	for _, want := range []string{
		"New",
		"New Folder",
		"New Text File",
		`openCreateDialog('folder')`,
		`openCreateDialog('file')`,
		`action="/mkdir"`,
		`action="/create-file"`,
		`/static/css/create.css`,
		`/static/js/create.js`,
		`id="new-folder-dialog"`,
		`id="new-file-dialog"`,
		"closeCreateDialog('new-folder-dialog')",
		"closeCreateDialog('new-file-dialog')",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("browse page missing %q", want)
		}
	}
}

// The create forms must carry the current canonical directory, so a submission
// never has to build a path on the client.
func TestBrowseCreateFormsCarryCurrentDirectory(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.MkdirAll(filepath.Join(root, "documents", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, body := browseBody(t, "/?path=documents%2Fprojects")

	// The search form and both dialogs all carry the same hidden path value.
	if got := strings.Count(body, `name="path" value="documents/projects"`); got < 3 {
		t.Errorf("current directory appears %d times, want at least 3 (search + two dialogs)", got)
	}
	if strings.Contains(body, `name="path" value="/documents/projects"`) {
		t.Errorf("current directory was rendered as an absolute path")
	}
	if strings.Contains(body, `name="path" value="documents/projects/"`) {
		t.Errorf("current directory was rendered with a trailing slash")
	}
}

// Existing actions must be untouched by the create UI.
func TestBrowseExistingActionsRemain(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, body := browseBody(t, "/")

	for _, want := range []string{
		`action="/"`,
		`name="search"`,
		`id="upload-form"`,
		`action="/upload?path=`,
		`href="/download?path=file.txt"`,
		`href="/view?path=file.txt"`,
		`href="/zip?path=folder"`,
		`onclick="renameFile(`,
		`action="/delete"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("existing action %q is missing", want)
		}
	}
}
