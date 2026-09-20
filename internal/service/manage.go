package service

import (
	"os"
	"path"
	"path/filepath"
)

// Delete removes a file or folder from the shared root.
//
// Intermediate symlinks and junctions are resolved and must stay inside the
// shared root. The final component is treated specially: if it is itself a
// symlink or junction, only the link is removed and its target is left
// untouched. A link whose target lies outside the shared root is rejected. The
// shared root itself cannot be deleted.
func Delete(rel string) error {
	logical, err := CleanRel(rel)
	if err != nil {
		return err
	}

	if logical == "" {
		return ErrRootOperation
	}

	target, err := resolveFinal(logical)
	if err != nil {
		return err
	}

	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}

	if isLinkLike(info) {
		return removeLink(target)
	}

	return os.RemoveAll(target)
}

// removeLink deletes a symlink or junction itself without following it. The
// link target must be inside the shared root; a link pointing outside the root
// is rejected. A dangling link is removed directly, because removing a link
// never affects its (nonexistent) target.
func removeLink(target string) error {
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		if os.IsNotExist(err) {
			return os.Remove(target)
		}
		return err
	}

	root, err := sharedRoot()
	if err != nil {
		return err
	}

	if !isWithin(root, resolved) {
		return ErrAccessDenied
	}

	return os.Remove(target)
}

// isLinkLike reports whether info describes a symbolic link, junction or other
// name-surrogate reparse point. On Windows, Go 1.23 and later report junctions
// as ModeIrregular rather than ModeSymlink, so both bits are checked.
func isLinkLike(info os.FileInfo) bool {
	mode := info.Mode()
	return mode&os.ModeSymlink != 0 || mode&os.ModeIrregular != 0
}

// Rename moves a file or folder to a new name within the same directory. Both
// the source and the destination parent are validated so neither can escape the
// shared root.
func Rename(rel string, newName string) error {
	logical, err := CleanRel(rel)
	if err != nil {
		return err
	}

	if logical == "" {
		return ErrRootOperation
	}

	safeName, err := CleanName(newName)
	if err != nil {
		return err
	}

	oldPath, err := ResolveExisting(logical)
	if err != nil {
		return err
	}

	parentDir := path.Dir(logical)
	if parentDir == "." {
		parentDir = ""
	}

	newPath, err := ResolveForCreate(path.Join(parentDir, safeName))
	if err != nil {
		return err
	}

	if _, err := os.Stat(newPath); err == nil {
		return ErrFileExists
	}

	return os.Rename(oldPath, newPath)
}
