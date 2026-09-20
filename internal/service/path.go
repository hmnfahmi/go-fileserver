package service

import (
	"errors"
	"go-fileserver/internal/config"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Errors returned by the path layer. Handlers map these to HTTP status codes
// and to client-safe messages so internal filesystem errors never reach the
// browser.
var (
	// ErrAccessDenied means the requested path resolves outside the shared
	// root (lexically or through a symlink/junction).
	ErrAccessDenied = errors.New("access denied")
	// ErrInvalidPath means the requested path is malformed or absolute.
	ErrInvalidPath = errors.New("invalid path")
	// ErrNotFound means the path does not exist.
	ErrNotFound = errors.New("file or folder not found")
	// ErrInvalidName means a supplied file/folder name is not a single,
	// usable path component.
	ErrInvalidName = errors.New("invalid name")
	// ErrRootOperation means the operation targets the shared root itself.
	ErrRootOperation = errors.New("operation not allowed on the shared root")
	// ErrNotDirectory means the target exists but is not a directory.
	ErrNotDirectory = errors.New("target is not a directory")
	// ErrIsDirectory means the target exists but is a directory.
	ErrIsDirectory = errors.New("target is a directory")
)

// CleanRel converts a request-controlled logical path into a canonical,
// slash-separated relative path. It is the single boundary between "URL path"
// and "filesystem path": everything above it deals in slash-separated logical
// paths, and only this package turns them into filesystem paths.
//
// It rejects anything that could denote a location outside the shared root:
//
//   - absolute POSIX paths ("/etc/passwd")
//   - Windows drive paths ("C:\\Windows", "C:/Windows", "C:relative")
//   - UNC paths ("\\\\server\\share")
//   - parent traversal that escapes the root ("../secret")
//
// Backslashes are normalised to forward slashes so the same logical path
// behaves identically on Windows and POSIX. The returned path is always
// relative and never contains "." or ".." components. The root is "".
func CleanRel(rel string) (string, error) {
	if strings.ContainsRune(rel, 0) {
		return "", ErrInvalidPath
	}

	normalized := strings.ReplaceAll(rel, "\\", "/")

	if hasRootOrVolume(normalized) {
		return "", ErrInvalidPath
	}

	cleaned := path.Clean(normalized)
	if cleaned == "." || cleaned == "/" {
		return "", nil
	}

	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrAccessDenied
	}

	return cleaned, nil
}

// hasRootOrVolume reports whether a slash-normalised path starts with a root
// or volume designator that would let it escape the shared root.
func hasRootOrVolume(p string) bool {
	if p == "" {
		return false
	}

	// Leading slash: POSIX absolute path or a UNC-style "//server/share".
	if p[0] == '/' {
		return true
	}

	// Drive letter followed by a colon: "C:", "C:/x", "C:relative".
	if len(p) >= 2 && isDriveLetter(p[0]) && p[1] == ':' {
		return true
	}

	return false
}

func isDriveLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// sharedRoot returns the absolute, symlink-resolved shared root. Resolving the
// root once means the configured path may itself be a symlink without breaking
// containment checks.
func sharedRoot() (string, error) {
	root, err := filepath.Abs(config.SharedPath)
	if err != nil {
		return "", err
	}

	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}

	return resolved, nil
}

// SafePath returns the absolute filesystem path for a logical path after
// lexical validation. It does not resolve symlinks, so it must not be used
// directly for filesystem access; use ResolveExisting for paths that should
// already exist and ResolveForCreate for destinations that may not exist yet.
func SafePath(rel string) (string, error) {
	cleaned, err := CleanRel(rel)
	if err != nil {
		return "", err
	}

	root, err := sharedRoot()
	if err != nil {
		return "", err
	}

	target := filepath.Join(root, filepath.FromSlash(cleaned))
	if !isWithin(root, target) {
		return "", ErrAccessDenied
	}

	return target, nil
}

// ResolveExisting returns the filesystem path of an existing logical path and
// verifies that, after resolving symlinks and junctions, it is still inside the
// shared root. Use it for browse, download, preview, delete and rename source
// operations.
func ResolveExisting(rel string) (string, error) {
	target, err := SafePath(rel)
	if err != nil {
		return "", err
	}

	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", err
	}

	root, err := sharedRoot()
	if err != nil {
		return "", err
	}

	if !isWithin(root, resolved) {
		return "", ErrAccessDenied
	}

	return resolved, nil
}

// ResolveForCreate returns the filesystem path of a destination that may not
// exist yet. It verifies that the destination's parent directory, after
// resolving symlinks and junctions, is still inside the shared root. Use it for
// upload destinations and rename targets.
//
// The final component is not resolved (it usually does not exist); callers
// create it with os.O_CREATE|os.O_EXCL, which refuses to follow an existing
// symlink.
func ResolveForCreate(rel string) (string, error) {
	return resolveFinal(rel)
}

// resolveFinal verifies that the parent directory of rel, after resolving
// intermediate symlinks and junctions, is inside the shared root, and returns
// the path of the final component beneath that resolved parent.
//
// The final component itself is deliberately not resolved so callers can
// inspect it (for example with os.Lstat) or create it without following an
// existing link.
func resolveFinal(rel string) (string, error) {
	target, err := SafePath(rel)
	if err != nil {
		return "", err
	}

	parent := filepath.Dir(target)

	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", err
	}

	root, err := sharedRoot()
	if err != nil {
		return "", err
	}

	if !isWithin(root, resolvedParent) {
		return "", ErrAccessDenied
	}

	return filepath.Join(resolvedParent, filepath.Base(target)), nil
}

// CleanName validates a user-supplied file or folder name and returns the
// single path component it denotes. Directory parts are stripped (browsers can
// send a full client path on upload) and separators, ".", ".." and empty names
// are rejected.
func CleanName(name string) (string, error) {
	if strings.ContainsRune(name, 0) {
		return "", ErrInvalidName
	}

	normalized := strings.ReplaceAll(name, "\\", "/")
	base := path.Base(normalized)

	if base == "" || base == "." || base == ".." || base == "/" {
		return "", ErrInvalidName
	}

	if strings.ContainsAny(base, `/\`) {
		return "", ErrInvalidName
	}

	return base, nil
}

// isWithin reports whether target is root or lies below root. It uses
// filepath.Rel rather than a string prefix so sibling directories with a
// similar name (for example C:\shared and C:\shared-other) are not confused.
func isWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}

	if rel == "." {
		return true
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// PublicMessage returns a client-safe description of a filesystem error. The
// original error should still be logged by the caller for diagnosis.
func PublicMessage(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrAccessDenied):
		return "Access denied"
	case errors.Is(err, ErrNotFound):
		return "File or folder not found"
	case errors.Is(err, os.ErrPermission):
		return "Access denied"
	case errors.Is(err, os.ErrNotExist):
		return "File or folder not found"
	case errors.Is(err, os.ErrExist):
		return "File already exists"
	case errors.Is(err, ErrInvalidPath):
		return "Invalid path"
	case errors.Is(err, ErrInvalidName):
		return "Invalid file name"
	case errors.Is(err, ErrRootOperation):
		return "Cannot modify the root shared folder"
	case errors.Is(err, ErrNotDirectory):
		return "Target is not a directory"
	case errors.Is(err, ErrIsDirectory):
		return "Target is a directory"
	case errors.Is(err, ErrFileExists):
		return "File already exists"
	default:
		return "Operation failed"
	}
}
