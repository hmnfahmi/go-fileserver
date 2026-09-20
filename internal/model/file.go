package model

import "time"

type FileItem struct {
	Name         string
	RelPath      string
	Size         int64
	SizeText     string
	IsDir        bool
	Modified     time.Time
	ModifiedText string
	Previewable  bool
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
}
