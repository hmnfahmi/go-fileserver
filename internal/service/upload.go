package service

import (
	"errors"
	"io"
	"os"
	"path"
)

var ErrFileExists = errors.New("file already exists")

// SaveUpload writes an uploaded file into the requested directory inside the
// shared root. The filename is reduced to a single path component and the
// destination is created with O_EXCL so an existing symlink is never followed.
func SaveUpload(dirRel string, filename string, r io.Reader) error {
	logicalDir, err := CleanRel(dirRel)
	if err != nil {
		return err
	}

	dir, err := ResolveExisting(logicalDir)
	if err != nil {
		return err
	}

	info, err := os.Stat(dir)
	if err != nil {
		// The destination directory may have been removed after resolution.
		return classifyFSError(err)
	}
	if !info.IsDir() {
		return ErrNotDirectory
	}

	safeName, err := CleanName(filename)
	if err != nil {
		return err
	}

	destPath, err := ResolveForCreate(path.Join(logicalDir, safeName))
	if err != nil {
		return err
	}

	f, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		// O_EXCL keeps its no-follow semantics; classifyFSError only turns the
		// resulting error into ErrFileExists / ErrNotFound / ErrAccessDenied.
		return classifyFSError(err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		os.Remove(destPath)
		return err
	}

	return nil
}
