package service

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"go-fileserver/internal/config"
)

// defaultMaxEditSize is used when the configuration has not been initialised
// (for example in a unit test that only sets config.SharedPath). The shipped
// default is the same value.
const defaultMaxEditSize int64 = 1 << 20 // 1 MiB

// tempEditPrefix marks temporary files created during an atomic save. They live
// in the target's own directory and are removed on every failure path.
const tempEditPrefix = ".go-fileserver-edit-"

// EditableExtensions is the single, central allowlist of file extensions that
// may be opened in the browser text editor. Extension matching is
// case-insensitive. It is deliberately narrower than "anything that can be
// decoded as UTF-8": a file is only editable when its type is on this list.
var EditableExtensions = map[string]bool{
	".txt":      true,
	".md":       true,
	".markdown": true,
	".json":     true,
	".yaml":     true,
	".yml":      true,
	".xml":      true,
	".csv":      true,
	".log":      true,
	".sql":      true,
	".go":       true,
	".js":       true,
	".ts":       true,
	".jsx":      true,
	".tsx":      true,
	".css":      true,
	".html":     true,
	".htm":      true,
}

// IsEditableExtension reports whether a lower-case extension (including the
// leading dot) is on the editor allowlist.
func IsEditableExtension(ext string) bool {
	return EditableExtensions[strings.ToLower(ext)]
}

// IsEditableName reports whether a bare file name has an editable extension.
// It is case-insensitive and uses only the final extension.
func IsEditableName(name string) bool {
	return IsEditableExtension(path.Ext(name))
}

// EditSizeLimit returns the configured maximum editable file size. A
// non-positive value (configuration not loaded, as in some tests) falls back to
// the built-in default so the limit can never be silently disabled.
func EditSizeLimit() int64 {
	if config.MaxEditSize > 0 {
		return config.MaxEditSize
	}
	return defaultMaxEditSize
}

// FileVersion is an opaque representation of a file's state at a point in time.
// It combines size, modification time and a SHA-256 of the content. Size and
// modification time alone are not reliable because filesystem timestamp
// resolution varies, so the content hash is the authoritative component.
//
// A version is always derived from the actual bytes on disk. It is sent to the
// browser and returned on save only so the server can recompute and compare it;
// it is never trusted as proof of anything by itself.
type FileVersion struct {
	Size    int64
	ModTime int64  // UnixNano
	Hash    string // hex-encoded SHA-256 of the content
}

// String encodes the version as a single opaque token.
func (v FileVersion) String() string {
	return strconv.FormatInt(v.Size, 10) + "-" +
		strconv.FormatInt(v.ModTime, 10) + "-" + v.Hash
}

// ParseFileVersion decodes a token produced by FileVersion.String. It rejects
// anything malformed so a tampered hidden field cannot cause an unexpected
// comparison.
func ParseFileVersion(s string) (FileVersion, bool) {
	parts := strings.SplitN(s, "-", 3)
	if len(parts) != 3 {
		return FileVersion{}, false
	}

	size, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || size < 0 {
		return FileVersion{}, false
	}

	mod, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return FileVersion{}, false
	}

	hash := parts[2]
	if len(hash) != sha256.Size*2 {
		return FileVersion{}, false
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return FileVersion{}, false
	}

	return FileVersion{Size: size, ModTime: mod, Hash: hash}, true
}

// EditableDocument is the result of opening a file for editing.
type EditableDocument struct {
	// Path is the canonical, slash-separated logical path.
	Path    string
	Name    string
	Content string
	Size    int64
	Version FileVersion
}

// ReadEditableFile opens a file for editing. It enforces the full policy:
//
//   - the logical path is validated through CleanRel;
//   - the final component is resolved without following a symlink/junction and
//     must be a regular file inside the shared root;
//   - the extension must be on the editor allowlist;
//   - the file must not exceed the configured edit-size limit;
//   - the bytes must be valid UTF-8.
//
// The size limit is enforced while reading (one byte past the limit is enough
// to detect an oversized file) so an arbitrarily large file is never loaded
// into memory. Binary files are never transcoded: invalid UTF-8 is rejected.
func ReadEditableFile(rel string) (*EditableDocument, error) {
	logical, target, err := resolveEditableTarget(rel)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(target)
	if err != nil {
		return nil, classifyFSError(err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, classifyFSError(err)
	}
	if info.IsDir() {
		return nil, ErrIsDirectory
	}
	if !info.Mode().IsRegular() {
		return nil, ErrNotRegular
	}

	limit := EditSizeLimit()
	if info.Size() > limit {
		return nil, ErrTooLarge
	}

	data, err := readBounded(f, limit)
	if err != nil {
		return nil, err
	}

	if !utf8.Valid(data) {
		return nil, ErrInvalidEncoding
	}

	sum := sha256.Sum256(data)

	return &EditableDocument{
		Path:    logical,
		Name:    path.Base(logical),
		Content: string(data),
		Size:    int64(len(data)),
		Version: FileVersion{
			Size:    int64(len(data)),
			ModTime: info.ModTime().UnixNano(),
			Hash:    hex.EncodeToString(sum[:]),
		},
	}, nil
}

// SaveEditableFile atomically replaces the content of an existing editable file
// after verifying that it has not changed since it was read.
//
// expected must be the version returned by ReadEditableFile. The current file
// state is recomputed from disk and compared; a mismatch returns ErrConflict and
// the existing file is left untouched. A missing target returns ErrNotFound and
// is never recreated.
//
// The new content is written to a uniquely named temporary file in the same
// directory, closed, given the original file's permission bits and then renamed
// over the target. A failed write therefore never leaves a partially written
// target. The rename provides the atomic replacement on both Unix and Windows;
// see the package documentation for the remaining compare-then-replace window.
func SaveEditableFile(rel string, content []byte, expected FileVersion) error {
	_, target, err := resolveEditableTarget(rel)
	if err != nil {
		return err
	}

	limit := EditSizeLimit()
	if int64(len(content)) > limit {
		return ErrTooLarge
	}

	info, err := os.Lstat(target)
	if err != nil {
		return classifyFSError(err)
	}

	current, err := readFileVersion(target, limit)
	if err != nil {
		return err
	}
	if current != expected {
		return ErrConflict
	}

	mode := info.Mode().Perm()

	dir := filepath.Dir(target)

	tmp, err := os.CreateTemp(dir, tempEditPrefix+"*.tmp")
	if err != nil {
		return classifyFSError(err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}

	// Closing must succeed before the temporary file is trusted as complete.
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}

	// Preserve the original permission bits. os.CreateTemp creates 0600, so
	// without this an edited file would silently lose its mode. Ownership and
	// other metadata are not touched.
	if err := os.Chmod(tmpName, mode); err != nil {
		os.Remove(tmpName)
		return classifyFSError(err)
	}

	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return classifyFSError(err)
	}

	return nil
}

// resolveEditableTarget validates an editable-file request and returns both the
// canonical logical path and the filesystem path to operate on.
//
// The final component is resolved with resolveFinal (parent resolved, final
// component untouched) and then inspected with Lstat. A symlink or junction at
// the final component is rejected rather than followed, so the editor can never
// be used to read or replace a link target outside the shared root. Intermediate
// links keep the project's existing policy: they are resolved and must stay
// inside the root.
func resolveEditableTarget(rel string) (string, string, error) {
	logical, err := CleanRel(rel)
	if err != nil {
		return "", "", err
	}
	if logical == "" {
		return "", "", ErrIsDirectory
	}

	target, err := resolveFinal(logical)
	if err != nil {
		return "", "", err
	}

	info, err := os.Lstat(target)
	if err != nil {
		return "", "", classifyFSError(err)
	}
	if isLinkLike(info) {
		return "", "", ErrAccessDenied
	}
	if info.IsDir() {
		return "", "", ErrIsDirectory
	}
	if !info.Mode().IsRegular() {
		return "", "", ErrNotRegular
	}

	if !IsEditableName(logical) {
		return "", "", ErrNotEditable
	}

	return logical, target, nil
}

// readFileVersion recomputes the version of an existing regular file, enforcing
// the size limit while reading. It is used to compare the current state against
// the version captured when the editor was opened.
func readFileVersion(target string, limit int64) (FileVersion, error) {
	f, err := os.Open(target)
	if err != nil {
		return FileVersion{}, classifyFSError(err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return FileVersion{}, classifyFSError(err)
	}
	if info.IsDir() {
		return FileVersion{}, ErrIsDirectory
	}
	if !info.Mode().IsRegular() {
		return FileVersion{}, ErrNotRegular
	}
	if info.Size() > limit {
		return FileVersion{}, ErrTooLarge
	}

	data, err := readBounded(f, limit)
	if err != nil {
		return FileVersion{}, err
	}

	sum := sha256.Sum256(data)

	return FileVersion{
		Size:    int64(len(data)),
		ModTime: info.ModTime().UnixNano(),
		Hash:    hex.EncodeToString(sum[:]),
	}, nil
}

// readBounded reads at most limit bytes and reports ErrTooLarge if the reader
// holds more. Reading limit+1 bytes distinguishes "exactly at the limit" from
// "over the limit" without buffering the whole file.
func readBounded(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, classifyFSError(err)
	}
	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}
	return data, nil
}
