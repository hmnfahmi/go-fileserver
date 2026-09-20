package service

import (
	"os"
	"path"
)

// Delete removes a file or folder from the shared root. The path is resolved
// with symlinks so a link pointing outside the shared root can never be
// followed, and the shared root itself cannot be deleted.
func Delete(rel string) error {
	logical, err := CleanRel(rel)
	if err != nil {
		return err
	}

	if logical == "" {
		return ErrRootOperation
	}

	target, err := ResolveExisting(logical)
	if err != nil {
		return err
	}

	return os.RemoveAll(target)
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
