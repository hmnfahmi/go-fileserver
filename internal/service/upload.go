package service

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrFileExists = errors.New("file already exists")

func SaveUpload(dirRel string, filename string, r io.Reader) error {
	dir, err := SafePath(dirRel)
	if err != nil {
		return err
	}

	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("target is not a directory")
	}

	normalized := strings.ReplaceAll(filename, "\\", "/")
	safeName := filepath.Base(normalized)
	if safeName == "" || safeName == "." || safeName == ".." || strings.ContainsAny(safeName, "/\\") {
		return errors.New("invalid file name")
	}

	if _, err := SafePath(filepath.Join(dirRel, safeName)); err != nil {
		return err
	}

	destPath := filepath.Join(dir, safeName)

	f, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			return ErrFileExists
		}
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		os.Remove(destPath)
		return err
	}

	return nil
}
