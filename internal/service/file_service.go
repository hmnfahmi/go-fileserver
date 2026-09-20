package service

import (
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
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrNotDirectory
	}

	opts = normalizeListOptions(opts)

	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}

	var files []model.FileItem
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		name := entry.Name()
		if opts.Search != "" {
			if !strings.Contains(
				strings.ToLower(name),
				strings.ToLower(opts.Search),
			) {
				continue
			}
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))

		files = append(files, model.FileItem{
			Name:         name,
			RelPath:      path.Join(logical, name),
			IsDir:        entry.IsDir(),
			Size:         info.Size(),
			SizeText:     formatter.FormatSize(info.Size()),
			Modified:     info.ModTime(),
			ModifiedText: info.ModTime().Format("2006-01-02"),
			Previewable:  !entry.IsDir() && PreviewableExtensions[ext],
		})
	}

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

		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})

	return files, nil
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
