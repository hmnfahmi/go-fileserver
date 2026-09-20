package service

import (
	"errors"
	"io"
	"os"
)

var ErrFileExists = errors.New("file already exists")

// SaveUpload writes an uploaded file into the requested directory inside the
// shared root. The filename is reduced to a single path component (browsers may
// send a full client path) and the destination is created with O_EXCL so an
// existing symlink is never followed.
//
// The parent-directory validation is shared with CreateDirectory and
// CreateEmptyFile through resolveCreateTarget, so every creation path enforces
// the same security boundary.
func SaveUpload(dirRel string, filename string, r io.Reader) error {
	safeName, err := CleanName(filename)
	if err != nil {
		return err
	}

	destPath, err := resolveCreateTarget(dirRel, safeName)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, createFileMode)
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
