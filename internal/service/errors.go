package service

import (
	"errors"
	"os"
)

// classifyFSError maps the filesystem errors that a validated operation can
// still encounter when the target changes between path resolution and the
// actual syscall onto the service's sentinel error model. A file or directory
// that disappears (or becomes unreadable) after it has been resolved must not
// surface as an unexpected internal failure.
//
// Only the three expected, recoverable classes are translated:
//
//   - a vanished target or parent directory -> ErrNotFound
//   - a denied target or parent directory   -> ErrAccessDenied
//   - an existing destination               -> ErrFileExists
//
// Every other error is returned unchanged so genuinely unexpected failures keep
// mapping to HTTP 500. The classification uses errors.Is only; it never
// inspects error strings and never returns a raw os.PathError to the client.
func classifyFSError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrNotExist):
		return ErrNotFound
	case errors.Is(err, os.ErrPermission):
		return ErrAccessDenied
	case errors.Is(err, os.ErrExist):
		return ErrFileExists
	default:
		return err
	}
}
