package service

import (
	"os"
	"path"
	"strings"
)

// createMode is the permission used for newly created directories and files. It
// matches the modes already used by uploads and by the rest of the service.
const (
	createDirMode  os.FileMode = 0o755
	createFileMode os.FileMode = 0o644
)

// resolveCreateTarget validates a destination directory and returns the safe
// filesystem path of a not-yet-existing child. It is the single reusable
// creation primitive shared by CreateDirectory, CreateEmptyFile and SaveUpload:
// every caller resolves its parent through the same boundary, so no caller can
// introduce a weaker path check.
//
// Security: the directory is reduced with CleanRel and resolved with
// ResolveExisting, so traversal and escaping symlinks/junctions are rejected
// before anything is created. The child is then resolved with ResolveForCreate,
// which resolves the parent chain (rejecting an escape) but deliberately leaves
// the final component unresolved so callers can create it exclusively.
//
// safeName must already be a single, validated path component (see CleanName and
// cleanEntryName); it is joined as one component and is never reinterpreted.
func resolveCreateTarget(relDir, safeName string) (string, error) {
	logicalDir, err := CleanRel(relDir)
	if err != nil {
		return "", err
	}

	dir, err := ResolveExisting(logicalDir)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(dir)
	if err != nil {
		// The directory may have been removed after resolution.
		return "", classifyFSError(err)
	}
	if !info.IsDir() {
		return "", ErrNotDirectory
	}

	return ResolveForCreate(path.Join(logicalDir, safeName))
}

// cleanEntryName validates a name supplied for a new directory or file. It builds
// on CleanName (the central name validator) but adds the rule that a creation
// name must be exactly one path component: a name containing a separator is
// rejected rather than silently reduced to its base name.
//
// That difference is deliberate. Uploads intentionally accept a client path and
// keep only the final component, whereas a creation form must not silently
// create a differently named entry than the user typed. Every unsafe shape
// (".", "..", empty, "/", "\", NUL, absolute, drive-qualified and UNC-style
// paths) is therefore rejected with ErrInvalidName.
func cleanEntryName(name string) (string, error) {
	if strings.ContainsRune(name, 0) {
		return "", ErrInvalidName
	}

	safe, err := CleanName(name)
	if err != nil {
		return "", err
	}

	// CleanName reduces "a/b" to "b"; require the input to already be the single
	// component it denotes. Backslashes are compared after the same
	// normalisation CleanName applies.
	if strings.ReplaceAll(name, `\`, "/") != safe {
		return "", ErrInvalidName
	}

	// Reuse the central lexical validator so root/volume designators that survive
	// CleanName as a bare base name (for example the drive-relative "C:foo") are
	// rejected rather than being passed to the filesystem layer.
	if _, err := CleanRel(safe); err != nil {
		return "", ErrInvalidName
	}

	return safe, nil
}

// CreateDirectory creates a new directory inside relDir. The name must be a
// single component. Creation is atomic: os.Mkdir fails with an "already exists"
// error if anything (including a symlink or junction) occupies the name, so an
// existing entry is never overwritten or followed.
//
// Errors are classified through the shared filesystem error model, so a conflict
// becomes ErrFileExists (HTTP 409), a missing parent becomes ErrNotFound and an
// escaping path becomes ErrAccessDenied.
func CreateDirectory(relDir, name string) error {
	safeName, err := cleanEntryName(name)
	if err != nil {
		return err
	}

	dest, err := resolveCreateTarget(relDir, safeName)
	if err != nil {
		return err
	}

	return classifyFSError(os.Mkdir(dest, createDirMode))
}

// CreateEmptyFile creates a new zero-byte file inside relDir. The name must be a
// single component.
//
// O_CREATE|O_EXCL is used so the existence check and the creation are a single
// atomic syscall: there is no check-then-create window and two concurrent
// callers cannot both succeed. O_EXCL also never follows a symlink or junction,
// so an existing link is reported as a conflict instead of being written
// through. An existing file is never overwritten and never truncated.
//
// No content is written: the editor populates the file in a later phase.
func CreateEmptyFile(relDir, name string) error {
	safeName, err := cleanEntryName(name)
	if err != nil {
		return err
	}

	dest, err := resolveCreateTarget(relDir, safeName)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, createFileMode)
	if err != nil {
		// classifyFSError turns the exclusive-create failure into ErrFileExists
		// (conflict), ErrNotFound (vanished parent) or ErrAccessDenied.
		return classifyFSError(err)
	}

	return classifyFSError(f.Close())
}
