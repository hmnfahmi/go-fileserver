package service

import (
	"errors"
	"go-fileserver/internal/config"
	"os"
	"path/filepath"
	"testing"
)

// withSharedRoot points config.SharedPath at dir for the duration of the test.
// Tests that call this must not run in parallel, because config.SharedPath is a
// package-level global.
func withSharedRoot(t *testing.T, dir string) {
	t.Helper()

	original := config.SharedPath
	config.SharedPath = dir
	t.Cleanup(func() { config.SharedPath = original })
}

func TestCleanRel(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{"empty is root", "", "", nil},
		{"dot is root", ".", "", nil},
		{"simple file", "a.txt", "a.txt", nil},
		{"nested", "a/b/c.txt", "a/b/c.txt", nil},
		{"trailing slash", "a/b/", "a/b", nil},
		{"redundant separators", "a//b", "a/b", nil},
		{"inner dot", "a/./b", "a/b", nil},
		{"inner parent stays inside", "a/../b", "b", nil},
		{"backslashes normalised", `a\b\c`, "a/b/c", nil},
		{"leading parent escapes", "../secret", "", ErrAccessDenied},
		{"nested parent escapes", "a/../../secret", "", ErrAccessDenied},
		{"parent alone", "..", "", ErrAccessDenied},
		{"posix absolute", "/etc/passwd", "", ErrInvalidPath},
		{"windows drive", `C:\Windows\win.ini`, "", ErrInvalidPath},
		{"windows drive forward slash", "C:/Windows", "", ErrInvalidPath},
		{"windows drive relative", "C:relative", "", ErrInvalidPath},
		{"unc path", `\\server\share`, "", ErrInvalidPath},
		{"null byte", "a\x00b", "", ErrInvalidPath},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CleanRel(tt.input)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("CleanRel(%q) err = %v, want %v", tt.input, err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("CleanRel(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("CleanRel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCleanName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"simple", "a.txt", "a.txt", false},
		{"keeps spaces", "my file.txt", "my file.txt", false},
		{"strips client path", `C:\Users\me\a.txt`, "a.txt", false},
		{"strips posix client path", "/tmp/a.txt", "a.txt", false},
		{"empty rejected", "", "", true},
		{"dot rejected", ".", "", true},
		{"parent rejected", "..", "", true},
		{"null byte rejected", "a\x00b", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CleanName(tt.input)

			if tt.wantErr {
				if !errors.Is(err, ErrInvalidName) {
					t.Fatalf("CleanName(%q) err = %v, want ErrInvalidName", tt.input, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("CleanName(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("CleanName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSafePathRejectsSiblingPrefix(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "shared")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	// A sibling directory whose name shares a prefix with the root must not be
	// reachable through a traversal.
	sibling := filepath.Join(parent, "shared-other")
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}

	withSharedRoot(t, root)

	if _, err := SafePath("../shared-other"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("SafePath traversal err = %v, want ErrAccessDenied", err)
	}
}

func TestSafePathInsideRoot(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	got, err := SafePath("a/b.txt")
	if err != nil {
		t.Fatalf("SafePath: %v", err)
	}

	want := filepath.Join(root, "a", "b.txt")
	if got != want {
		t.Errorf("SafePath = %q, want %q", got, want)
	}
}

func TestResolveExistingRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	withSharedRoot(t, root)

	if _, err := ResolveExisting("link.txt"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("ResolveExisting symlink err = %v, want ErrAccessDenied", err)
	}
}

func TestResolveExistingAllowsInternalSymlink(t *testing.T) {
	root := t.TempDir()

	target := filepath.Join(root, "real.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	withSharedRoot(t, root)

	got, err := ResolveExisting("link.txt")
	if err != nil {
		t.Fatalf("ResolveExisting: %v", err)
	}
	if got != target {
		t.Errorf("ResolveExisting = %q, want %q", got, target)
	}
}

func TestResolveExistingNotFound(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if _, err := ResolveExisting("missing.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ResolveExisting err = %v, want ErrNotFound", err)
	}
}

func TestResolveForCreateRejectsSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	withSharedRoot(t, root)

	if _, err := ResolveForCreate("escape/new.txt"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("ResolveForCreate err = %v, want ErrAccessDenied", err)
	}
}

func TestPublicMessageHidesInternals(t *testing.T) {
	if got := PublicMessage(ErrAccessDenied); got != "Access denied" {
		t.Errorf("PublicMessage(ErrAccessDenied) = %q", got)
	}

	raw := errors.New(`open D:\secrets\file.txt: access is denied`)
	if got := PublicMessage(raw); got != "Operation failed" {
		t.Errorf("PublicMessage(raw) = %q, want generic message", got)
	}
}
