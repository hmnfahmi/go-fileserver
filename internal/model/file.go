package model

import (
	"context"
	"time"
)

type FileItem struct {
	Name         string
	RelPath      string
	Size         int64
	SizeText     string
	IsDir        bool
	Modified     time.Time
	ModifiedText string
	Previewable  bool
	// Kind is a presentation-only classification used to pick a listing icon.
	// It never affects filesystem access and is excluded from any JSON output.
	Kind FileKind `json:"-"`
}

type SortBy string

const (
	SortByName     SortBy = "name"
	SortBySize     SortBy = "size"
	SortByModified SortBy = "modified"
)

type SortOrder string

const (
	SortAsc  SortOrder = "asc"
	SortDesc SortOrder = "desc"
)

type ListOptions struct {
	Path      string
	Search    string
	SortBy    SortBy
	SortOrder SortOrder
	// Context, when non-nil, lets a recursive search stop early when the HTTP
	// request is cancelled. A nil Context is treated as context.Background().
	Context context.Context
}
