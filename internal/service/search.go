package service

import (
	"context"
	"errors"
	"go-fileserver/internal/model"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// matchesSearch reports whether name contains the search term. Matching stays
// exactly what the flat listing has always done: case-insensitive and
// substring-based, against the entry's base name. Recursive search only changes
// how far the listing looks, never what counts as a match.
func matchesSearch(name, needle string) bool {
	return strings.Contains(strings.ToLower(name), needle)
}

// searchRecursive returns every descendant of target whose base name matches
// opts.Search. Both arguments are already validated by List: target is the
// symlink-resolved selected directory and logical is its canonical,
// slash-separated path relative to the shared root.
//
// Security: the selected root was resolved with ResolveExisting. Every
// descendant is re-validated with isWithin and link-like entries are skipped, so
// a descendant link can never widen the walk outside the shared root. This is
// the same link policy as the archive walker (walkZipEntries).
func searchRecursive(ctx context.Context, target, logical string, opts model.ListOptions) ([]model.FileItem, error) {
	root, err := sharedRoot()
	if err != nil {
		return nil, err
	}

	needle := strings.ToLower(opts.Search)

	var files []model.FileItem
	if err := walkSearch(ctx, root, target, logical, needle, &files); err != nil {
		return nil, err
	}

	return files, nil
}

// walkSearch performs a depth-first walk of dir. It appends a FileItem for every
// genuine file or directory whose base name matches needle, and recurses into
// every genuine directory whether or not its own name matches, so a match nested
// below a non-matching parent is still found.
//
// The walk only ever descends into entries that are neither symlinks nor
// junctions, so a link cannot redirect it. os.ReadDir returns entries sorted by
// name, which keeps the walk order deterministic; the caller re-sorts the
// flattened result anyway.
func walkSearch(ctx context.Context, root, dir, logical, needle string, out *[]model.FileItem) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		// Only descendants reach this point: List reports errors for the
		// selected directory itself. A subtree that disappears or becomes
		// unreadable during the walk is skipped rather than failing an
		// otherwise useful search. Unexpected errors are still returned so a
		// genuine bug is not hidden behind a silently empty result.
		classified := classifyFSError(err)
		if errors.Is(classified, ErrNotFound) || errors.Is(classified, ErrAccessDenied) {
			return nil
		}
		return classified
	}

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}

		// Info reports the entry itself, so a symlink is visible as a link and
		// a dangling link is never followed.
		info, err := entry.Info()
		if err != nil {
			// The entry vanished between ReadDir and Info; skip it rather than
			// aborting the whole search for an entry that no longer exists.
			continue
		}

		if isLinkLike(info) {
			// Links and junctions are never followed or reported. Following one
			// is exactly the escape this walk must prevent, and reporting one
			// would expose a result whose action-time validation could only
			// fail. In-root, escaping and dangling links all behave the same.
			continue
		}

		name := entry.Name()
		childDir := filepath.Join(dir, name)
		if !isWithin(root, childDir) {
			// Defense-in-depth: a name that escaped dir is never reported or
			// descended into. List validates the selected root first, so this
			// should be unreachable in normal operation.
			continue
		}

		relPath := path.Join(logical, name)

		if info.IsDir() {
			if matchesSearch(name, needle) {
				*out = append(*out, fileItemFromEntry(name, relPath, info))
			}

			if err := walkSearch(ctx, root, childDir, relPath, needle, out); err != nil {
				return err
			}
			continue
		}

		if !info.Mode().IsRegular() {
			// Sockets, devices and FIFOs are not browsable entries.
			continue
		}

		if matchesSearch(name, needle) {
			*out = append(*out, fileItemFromEntry(name, relPath, info))
		}
	}

	return nil
}
