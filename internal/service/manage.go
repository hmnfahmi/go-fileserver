package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func Delete(rel string) error {
	if strings.TrimSpace(rel) == "" || rel == "." {
		return errors.New("cannot delete the root shared folder")
	}

	target, err := SafePath(rel)
	if err != nil {
		return err
	}

	if _, err := os.Stat(target); err != nil {
		return err
	}

	return os.RemoveAll(target)
}

func Rename(rel string, newName string) error {
	if strings.TrimSpace(rel) == "" || rel == "." {
		return errors.New("cannot rename the root shared folder")
	}

	oldPath, err := SafePath(rel)
	if err != nil {
		return err
	}

	if _, err := os.Stat(oldPath); err != nil {
		return err
	}

	normalized := strings.ReplaceAll(newName, "\\", "/")
	safeName := filepath.Base(normalized)
	if safeName == "" || safeName == "." || safeName == ".." || strings.ContainsAny(safeName, "/\\") {
		return errors.New("invalid new name")
	}

	parentRel := filepath.Dir(rel)
	if parentRel == "." {
		parentRel = ""
	}

	newRel := filepath.Join(parentRel, safeName)
	newPath, err := SafePath(newRel)
	if err != nil {
		return err
	}

	if _, err := os.Stat(newPath); err == nil {
		return ErrFileExists
	}

	return os.Rename(oldPath, newPath)
}
