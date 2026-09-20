package service

import (
	"errors"
	"go-fileserver/internal/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListUsesSlashSeparatedRelPaths(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "docs", "readme.txt"), "hi")

	items, err := List(model.ListOptions{Path: "docs"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].RelPath != "docs/readme.txt" {
		t.Errorf("RelPath = %q, want %q", items[0].RelPath, "docs/readme.txt")
	}
	if strings.ContainsRune(items[0].RelPath, filepath.Separator) && filepath.Separator != '/' {
		t.Errorf("RelPath contains a platform separator: %q", items[0].RelPath)
	}
}

func TestListRejectsFileAsDirectory(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "a.txt"), "x")

	if _, err := List(model.ListOptions{Path: "a.txt"}); !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("List(file) err = %v, want ErrNotDirectory", err)
	}
}

func TestListRejectsEscape(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if _, err := List(model.ListOptions{Path: "../"}); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("List escape err = %v, want ErrAccessDenied", err)
	}
}

func TestDeleteRejectsRoot(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	for _, rel := range []string{"", ".", "./"} {
		if err := Delete(rel); !errors.Is(err, ErrRootOperation) {
			t.Errorf("Delete(%q) err = %v, want ErrRootOperation", rel, err)
		}
	}

	if _, err := os.Stat(root); err != nil {
		t.Fatalf("shared root was removed: %v", err)
	}
}

func TestDeleteRemovesFile(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	target := filepath.Join(root, "a.txt")
	writeFile(t, target, "x")

	if err := Delete("a.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("file still exists after delete")
	}
}

func TestRenameRejectsRoot(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := Rename("", "new"); !errors.Is(err, ErrRootOperation) {
		t.Fatalf("Rename root err = %v, want ErrRootOperation", err)
	}
}

func TestRenameConflict(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "a.txt"), "a")
	writeFile(t, filepath.Join(root, "b.txt"), "b")

	if err := Rename("a.txt", "b.txt"); !errors.Is(err, ErrFileExists) {
		t.Fatalf("Rename conflict err = %v, want ErrFileExists", err)
	}
}

func TestRenameInvalidName(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "a.txt"), "a")

	if err := Rename("a.txt", ".."); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("Rename invalid name err = %v, want ErrInvalidName", err)
	}
}

func TestRenameMovesFile(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "a.txt"), "a")

	if err := Rename("a.txt", "b.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "b.txt")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
}

func TestSaveUploadRejectsTraversalName(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	// Browsers can send a full client path; CleanName strips the directory part.
	if err := SaveUpload("", `C:\Users\me\a.txt`, strings.NewReader("data")); err != nil {
		t.Fatalf("SaveUpload: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("uploaded file missing: %v", err)
	}
}

func TestSaveUploadConflict(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	writeFile(t, filepath.Join(root, "a.txt"), "existing")

	if err := SaveUpload("", "a.txt", strings.NewReader("new")); !errors.Is(err, ErrFileExists) {
		t.Fatalf("SaveUpload conflict err = %v, want ErrFileExists", err)
	}
}

func TestSaveUploadRejectsSymlinkDestination(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	secret := filepath.Join(outside, "secret.txt")
	writeFile(t, secret, "top secret")

	link := filepath.Join(root, "escape.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	withSharedRoot(t, root)

	err := SaveUpload("", "escape.txt", strings.NewReader("overwrite"))
	if err == nil {
		t.Fatal("SaveUpload followed a symlinked destination")
	}
	if !errors.Is(err, ErrFileExists) {
		t.Fatalf("SaveUpload err = %v, want ErrFileExists", err)
	}

	// The file the symlink pointed at must be untouched.
	content, readErr := os.ReadFile(secret)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "top secret" {
		t.Fatalf("symlink target was overwritten: %q", content)
	}
}
