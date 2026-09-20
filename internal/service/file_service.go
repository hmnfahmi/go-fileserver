package service

import (
	"context"
	"go-fileserver/internal/formatter"
	"go-fileserver/internal/model"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func List(opts model.ListOptions) ([]model.FileItem, error) {
	logical, err := CleanRel(opts.Path)
	if err != nil {
		return nil, err
	}

	target, err := ResolveExisting(logical)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(target)
	if err != nil {
		// The directory may have been removed after resolution.
		return nil, classifyFSError(err)
	}
	if !info.IsDir() {
		return nil, ErrNotDirectory
	}

	opts = normalizeListOptions(opts)

	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}

	var files []model.FileItem

	if opts.Search != "" {
		// Search descends recursively below the selected directory. The selected
		// directory is already resolved and validated above; descendants are
		// validated by the walk itself.
		files, err = searchRecursive(ctx, target, logical, opts)
	} else {
		files, err = readDirectory(target, logical)
	}
	if err != nil {
		return nil, err
	}

	sortFileItems(files, opts)

	return files, nil
}

// readDirectory lists the direct children of target for normal browsing.
// Recursive search uses searchRecursive instead.
func readDirectory(target, logical string) ([]model.FileItem, error) {
	entries, err := os.ReadDir(target)
	if err != nil {
		// The directory may have been removed or become unreadable between
		// Stat and ReadDir.
		return nil, classifyFSError(err)
	}

	var files []model.FileItem
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		files = append(files, fileItemFromEntry(entry.Name(), path.Join(logical, entry.Name()), info))
	}

	return files, nil
}

// fileItemFromEntry builds the view model for a single directory entry. relPath
// is the canonical, slash-separated path relative to the shared root and is the
// only value used to build action links, so a recursive result keeps its full
// location instead of collapsing to its base name.
func fileItemFromEntry(name, relPath string, info os.FileInfo) model.FileItem {
	isDir := info.IsDir()
	ext := strings.ToLower(filepath.Ext(name))

	return model.FileItem{
		Name:         name,
		RelPath:      relPath,
		IsDir:        isDir,
		Size:         info.Size(),
		SizeText:     formatter.FormatSize(info.Size()),
		Modified:     info.ModTime(),
		ModifiedText: info.ModTime().Format("2006-01-02"),
		Previewable:  !isDir && ClassifyPreview(ext) != PreviewNone,
		Kind:         model.ClassifyFileKind(name, isDir),
	}
}

// sortFileItems orders a listing deterministically: directories first, then the
// requested sort key and order, then a case-insensitive name fallback, then the
// canonical relative path. The final relative-path tiebreak makes recursive
// results stable even when identically named entries appear in different
// directories, independent of traversal order.
func sortFileItems(files []model.FileItem, opts model.ListOptions) {
	sort.SliceStable(files, func(i, j int) bool {
		a := files[i]
		b := files[j]

		if a.IsDir != b.IsDir {
			return a.IsDir
		}

		desc := opts.SortOrder == model.SortDesc
		switch opts.SortBy {
		case model.SortBySize:
			if a.Size != b.Size {
				if desc {
					return a.Size > b.Size
				}
				return a.Size < b.Size
			}
		case model.SortByModified:
			if !a.Modified.Equal(b.Modified) {
				if desc {
					return a.Modified.After(b.Modified)
				}
				return a.Modified.Before(b.Modified)
			}
		default:
			na := strings.ToLower(a.Name)
			nb := strings.ToLower(b.Name)

			if na != nb {
				if desc {
					return na > nb
				}
				return na < nb
			}
		}

		na := strings.ToLower(a.Name)
		nb := strings.ToLower(b.Name)
		if na != nb {
			return na < nb
		}

		return a.RelPath < b.RelPath
	})
}

func normalizeListOptions(opts model.ListOptions) model.ListOptions {
	if opts.SortBy == "" {
		opts.SortBy = model.SortByName
	}

	switch opts.SortBy {
	case model.SortByName, model.SortBySize, model.SortByModified:
	default:
		opts.SortBy = model.SortByName
	}

	if opts.SortOrder == "" {
		opts.SortOrder = model.SortAsc
	}

	switch opts.SortOrder {
	case model.SortAsc, model.SortDesc:
	default:
		opts.SortOrder = model.SortAsc
	}

	return opts
}
