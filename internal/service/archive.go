package service

import (
	"archive/zip"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// ZipEntry is one filesystem object scheduled to be written into an archive.
//
// Name is the normalised, slash-separated, relative archive entry name. Target
// is the absolute on-disk path to read the data from. IsDir marks a directory
// entry, which carries a trailing slash in Name so extractors can recreate the
// tree (including empty directories).
type ZipEntry struct {
	Name   string
	Target string
	IsDir  bool
}

// PrepareZip validates a logical path and returns the complete list of archive
// entries plus a safe base name for the download.
//
// The entire traversal happens up front, before the caller starts streaming, so
// a missing, escaping or non-archivable target is reported as a clean HTTP error
// instead of a half-written archive. The entry list is bounded by the number of
// filesystem objects, not by their contents; no file data is buffered.
//
// Security: the selected path is resolved with ResolveExisting (the existing
// boundary, including symlink resolution and root containment). Every descendant
// is then re-validated. Symlinks and junctions are never followed and never
// archived; see walkZipEntries for the policy.
func PrepareZip(rel string) ([]ZipEntry, string, error) {
	logical, err := CleanRel(rel)
	if err != nil {
		return nil, "", err
	}

	target, err := ResolveExisting(logical)
	if err != nil {
		return nil, "", err
	}

	info, err := os.Stat(target)
	if err != nil {
		// The target may have been removed after it was resolved.
		return nil, "", classifyFSError(err)
	}

	download := archiveBaseName(logical, target)

	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			// Sockets, devices and FIFOs have no portable archive
			// representation. Report them as "not a directory" rather than
			// inventing a new error class.
			return nil, "", ErrNotDirectory
		}

		// Prefer the user-visible name so a selected symlink keeps its own name
		// rather than the resolved target's name.
		base := path.Base(target)
		if logical != "" {
			base = path.Base(logical)
		}

		entry, ok := archiveEntryName("", base)
		if !ok {
			return nil, "", ErrInvalidName
		}

		return []ZipEntry{{Name: entry, Target: target}}, download, nil
	}

	root, err := sharedRoot()
	if err != nil {
		return nil, "", err
	}

	used := make(map[string]struct{})

	entries, err := walkZipEntries(root, target, "", used)
	if err != nil {
		return nil, "", err
	}

	// os.ReadDir already returns each directory sorted by name, but sorting the
	// flattened list keeps archives reproducible regardless of traversal order.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	return entries, download, nil
}

// WriteZip streams entries into w as a ZIP archive. It never buffers the whole
// archive: each file is copied directly into the archive writer, which writes
// through to the underlying ResponseWriter.
//
// A file that disappears after the entry list was built is reported as
// ErrNotFound rather than being silently dropped, so an incomplete archive is
// never presented as a successful one. Once the first byte has been written the
// caller can no longer change the HTTP status; that limitation is documented on
// the handler.
func WriteZip(w io.Writer, entries []ZipEntry) error {
	zw := zip.NewWriter(w)

	for _, entry := range entries {
		if entry.IsDir {
			header := &zip.FileHeader{Name: entry.Name, Method: zip.Store}
			header.SetMode(os.ModeDir | 0o755)

			if _, err := zw.CreateHeader(header); err != nil {
				return err
			}
			continue
		}

		if err := writeZipFile(zw, entry); err != nil {
			return err
		}
	}

	return zw.Close()
}

func writeZipFile(zw *zip.Writer, entry ZipEntry) error {
	f, err := os.Open(entry.Target)
	if err != nil {
		// The file disappeared between listing and copying.
		return classifyFSError(err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return classifyFSError(err)
	}

	header := &zip.FileHeader{
		Name:     entry.Name,
		Method:   zip.Deflate,
		Modified: info.ModTime(),
	}
	header.SetMode(0o644)

	dst, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}

	if _, err := io.Copy(dst, f); err != nil {
		return classifyFSError(err)
	}

	return nil
}

// walkZipEntries recursively collects entries for dir. prefix is the archive
// path accumulated so far (empty for the selected directory itself). used holds
// the archive names already claimed anywhere in the archive so two filesystem
// names can never collapse onto one entry.
//
// Symlink / junction policy:
//   - Symlinks and junctions are never followed and never archived, in or out
//     of the shared root. Following a link is exactly the escape this feature
//     must prevent, and storing the link itself would be non-portable and
//     ambiguous on extraction.
//   - In-root symlinks, dangling symlinks and escaping symlinks therefore all
//     behave identically: they are deterministically skipped.
//   - Because every child is a genuine non-link directory or regular file, a
//     descent stays inside dir, which ResolveExisting already proved is inside
//     the shared root.
//
// Collision policy: sanitising a backslash to "_" is not injective, so
// "a\b.txt" and "a_b.txt" would otherwise both become "a_b.txt". Literal names
// are therefore processed first so the file whose real name needs no rewriting
// keeps its exact entry name; any later name that maps onto an already claimed
// archive name is skipped deterministically rather than duplicating or
// overwriting an entry.
func walkZipEntries(root, dir, prefix string, used map[string]struct{}) ([]ZipEntry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		// The directory may have been removed or become unreadable after it was
		// resolved. Surface the classified error instead of a partial archive.
		return nil, classifyFSError(err)
	}

	// os.ReadDir already sorts by name; stable-sort literal names ahead of
	// sanitised ones so the deterministic winner is the name that is unchanged.
	sort.SliceStable(dirEntries, func(i, j int) bool {
		li := !strings.ContainsRune(dirEntries[i].Name(), '\\')
		lj := !strings.ContainsRune(dirEntries[j].Name(), '\\')
		if li != lj {
			return li
		}
		return dirEntries[i].Name() < dirEntries[j].Name()
	})

	var out []ZipEntry

	for _, dirEntry := range dirEntries {
		// DirEntry.Info reports the entry itself, not its target, so a symlink
		// is visible as a link.
		info, err := dirEntry.Info()
		if err != nil {
			// The entry vanished between ReadDir and Info; skip it rather than
			// aborting the archive for an entry that no longer exists.
			continue
		}

		if isLinkLike(info) {
			continue
		}

		entryName, ok := archiveEntryName(prefix, dirEntry.Name())
		if !ok {
			// A name that cannot be represented safely is skipped
			// deterministically; it is never reinterpreted as a path.
			continue
		}

		target := filepath.Join(dir, dirEntry.Name())
		if !isWithin(root, target) {
			continue
		}

		if dirEntry.IsDir() {
			if !claimArchiveName(used, entryName) {
				continue
			}

			out = append(out, ZipEntry{Name: entryName + "/", Target: target, IsDir: true})

			children, err := walkZipEntries(root, target, entryName, used)
			if err != nil {
				return nil, err
			}
			out = append(out, children...)
			continue
		}

		if !info.Mode().IsRegular() {
			// Sockets, devices and FIFOs are skipped; they cannot be archived
			// portably and reading them could block.
			continue
		}

		if !claimArchiveName(used, entryName) {
			continue
		}

		out = append(out, ZipEntry{Name: entryName, Target: target, IsDir: false})
	}

	return out, nil
}

// claimArchiveName records an archive entry name and reports whether it was
// still free. The trailing slash of a directory entry is ignored so a file and a
// directory that sanitise to the same name are treated as the same archive path.
func claimArchiveName(used map[string]struct{}, entryName string) bool {
	key := strings.TrimSuffix(entryName, "/")
	if _, exists := used[key]; exists {
		return false
	}

	used[key] = struct{}{}
	return true
}

// archiveEntryName joins a directory prefix and a single filesystem name into a
// normalised archive entry name, or reports that the name cannot be represented
// safely.
//
// os.ReadDir never returns a name containing "/", but on POSIX a name may contain
// a backslash. A literal backslash would be interpreted as a path separator by
// some Windows extractors, so it is replaced with "_" instead of being allowed to
// leak into the entry name. This cannot introduce traversal: the replacement is
// not a separator, so "..\.." becomes the harmless ".._..".
func archiveEntryName(prefix, name string) (string, bool) {
	if name == "" || name == "." || name == ".." {
		return "", false
	}
	if strings.ContainsRune(name, 0) || strings.ContainsRune(name, '/') {
		return "", false
	}

	name = strings.ReplaceAll(name, `\`, "_")

	entry := name
	if prefix != "" {
		entry = prefix + "/" + name
	}

	if !validArchiveEntryName(entry) {
		return "", false
	}

	return entry, true
}

// validArchiveEntryName reports whether name is a safe, relative, slash-
// separated archive entry name: not absolute, no drive or UNC designator, no
// backslash and no "." or ".." component. It is a final assertion over the
// assembled name, never the primary containment mechanism.
func validArchiveEntryName(name string) bool {
	if name == "" || strings.ContainsRune(name, '\\') {
		return false
	}
	if strings.HasPrefix(name, "/") {
		return false
	}
	if len(name) >= 2 && name[1] == ':' && isDriveLetter(name[0]) {
		return false
	}

	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" {
		return false
	}

	for _, part := range strings.Split(trimmed, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}

	return true
}

// archiveBaseName chooses the download name for an archive. It uses the logical
// (user-visible) base name so the name the user clicked is preserved even when
// the selected directory is itself a symlink. The shared root has no logical
// base name, so it falls back to the resolved directory name, then "shared".
func archiveBaseName(logical, target string) string {
	if logical != "" {
		if base := path.Base(logical); base != "" && base != "." && base != "/" {
			return base
		}
	}

	normalized := strings.ReplaceAll(target, `\`, "/")
	if base := path.Base(normalized); base != "" && base != "." && base != "/" {
		return base
	}

	return "shared"
}
