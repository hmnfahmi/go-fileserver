package service

import (
	"errors"
	"fmt"
	"go-fileserver/internal/model"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F4.4. classifyFSError is the single place where filesystem errors that a
// validated operation can still hit are normalised onto the sentinel model.
func TestClassifyFSError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"nil stays nil", nil, nil},
		{"not exist", os.ErrNotExist, ErrNotFound},
		{"fs.ErrNotExist", fs.ErrNotExist, ErrNotFound},
		{"wrapped not exist", fmt.Errorf("lstat: %w", os.ErrNotExist), ErrNotFound},
		{"path error not exist", &os.PathError{Op: "open", Path: "/secret", Err: os.ErrNotExist}, ErrNotFound},
		{"permission", os.ErrPermission, ErrAccessDenied},
		{"fs.ErrPermission", fs.ErrPermission, ErrAccessDenied},
		{"wrapped permission", fmt.Errorf("open: %w", os.ErrPermission), ErrAccessDenied},
		{"exist", os.ErrExist, ErrFileExists},
		{"wrapped exist", fmt.Errorf("rename: %w", os.ErrExist), ErrFileExists},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyFSError(tc.err)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("classifyFSError(nil) = %v, want nil", got)
				}
				return
			}
			if !errors.Is(got, tc.want) {
				t.Errorf("classifyFSError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// An unexpected filesystem error must pass through untouched so the handler can
// still map it to 500.
func TestClassifyFSErrorLeavesUnexpectedErrorsUnchanged(t *testing.T) {
	for _, err := range []error{
		errors.New("disk on fire"),
		fs.ErrInvalid,
		fs.ErrClosed,
	} {
		if got := classifyFSError(err); got != err {
			t.Errorf("classifyFSError(%v) = %v, want the same error", err, got)
		}
	}
}

// A target removed after resolution must map to ErrNotFound, not a raw ENOENT.
// Delete resolves the parent and then Lstats the final component, so this
// exercises classifyFSError on the Lstat result.
func TestDeleteDisappearedTargetIsNotFound(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.WriteFile(filepath.Join(root, "gone.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "gone.txt")); err != nil {
		t.Fatal(err)
	}

	if err := Delete("gone.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete(disappeared) err = %v, want ErrNotFound", err)
	}
}

func TestListDisappearedDirectoryIsNotFound(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "gone"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "gone")); err != nil {
		t.Fatal(err)
	}

	_, err := List(model.ListOptions{Path: "gone"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("List(disappeared) err = %v, want ErrNotFound", err)
	}
}

func TestRenameDisappearedSourceIsNotFound(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.WriteFile(filepath.Join(root, "gone.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "gone.txt")); err != nil {
		t.Fatal(err)
	}

	if err := Rename("gone.txt", "other.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Rename(disappeared source) err = %v, want ErrNotFound", err)
	}
}

// The destination parent is resolved before the rename; if it is gone the
// operation must be a not-found rather than an unexpected failure.
func TestRenameIntoDisappearedParentIsNotFound(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "src.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "sub")); err != nil {
		t.Fatal(err)
	}

	if err := Rename("sub/src.txt", "renamed.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Rename(parent disappeared) err = %v, want ErrNotFound", err)
	}
}

func TestSaveUploadMissingDirectoryIsNotFound(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.Mkdir(filepath.Join(root, "updir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "updir")); err != nil {
		t.Fatal(err)
	}

	err := SaveUpload("updir", "file.txt", strings.NewReader("data"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("SaveUpload(missing dir) err = %v, want ErrNotFound", err)
	}
}

// permissionsEnforced reports whether this process actually observes POSIX
// permission bits. It returns false for root or a platform/filesystem that
// ignores them, so callers can skip instead of asserting platform-specific
// behaviour.
func permissionsEnforced(t *testing.T, dir string) bool {
	t.Helper()

	probe := filepath.Join(dir, ".permission-probe")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		f.Close()
		os.Remove(probe)
		return false
	}

	return errors.Is(err, os.ErrPermission)
}

// lockDir removes all permission bits and registers a cleanup that restores
// them, so t.TempDir's own RemoveAll can still delete the directory afterwards.
// Cleanups run LIFO, so the restore registered here executes before the
// TempDir cleanup registered earlier in the test.
func lockDir(t *testing.T, dir string, mode os.FileMode) {
	t.Helper()

	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
}

// The remaining tests pin the concrete call sites where classifyFSError now
// translates a raw permission error into ErrAccessDenied. They exercise the
// ReadDir, Lstat/RemoveAll, OpenFile and Rename syscalls respectively.

func TestListPermissionDeniedIsAccessDenied(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	lockDir(t, locked, 0o000)
	if !permissionsEnforced(t, locked) {
		t.Skip("filesystem permissions are not enforced for this user")
	}

	_, err := List(model.ListOptions{Path: "locked"})
	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("List(denied) err = %v, want ErrAccessDenied", err)
	}
}

func TestDeletePermissionDeniedIsAccessDenied(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockDir(t, locked, 0o555)
	if !permissionsEnforced(t, locked) {
		t.Skip("filesystem permissions are not enforced for this user")
	}

	if err := Delete("locked/a.txt"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("Delete(denied) err = %v, want ErrAccessDenied", err)
	}
}

func TestSaveUploadPermissionDeniedIsAccessDenied(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	lockDir(t, locked, 0o555)
	if !permissionsEnforced(t, locked) {
		t.Skip("filesystem permissions are not enforced for this user")
	}

	err := SaveUpload("locked", "new.txt", strings.NewReader("data"))
	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("SaveUpload(denied) err = %v, want ErrAccessDenied", err)
	}
}

func TestRenamePermissionDeniedIsAccessDenied(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockDir(t, locked, 0o555)
	if !permissionsEnforced(t, locked) {
		t.Skip("filesystem permissions are not enforced for this user")
	}

	if err := Rename("locked/a.txt", "renamed.txt"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("Rename(denied) err = %v, want ErrAccessDenied", err)
	}
}
