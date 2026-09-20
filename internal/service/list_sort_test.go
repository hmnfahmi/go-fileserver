package service

import (
	"go-fileserver/internal/model"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// listFixture builds a deterministic directory tree under the shared root. It
// uses os.Chtimes so modified-time ordering does not depend on wall-clock
// scheduling. Names, sizes and mtimes are all distinct to make the expected
// ordering unambiguous.
//
// Tree:
//
//	adir/            (directory)
//	zeta/            (directory)
//	big.bin          300 bytes
//	m.md             20 bytes
//	small.txt        5 bytes
func listFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.MkdirAll(filepath.Join(root, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "zeta"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(root, "big.bin"), string(make([]byte, 300)))
	writeFile(t, filepath.Join(root, "m.md"), string(make([]byte, 20)))
	writeFile(t, filepath.Join(root, "small.txt"), string(make([]byte, 5)))

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	times := map[string]time.Time{
		"adir":      base.Add(5 * time.Hour),
		"zeta":      base.Add(1 * time.Hour),
		"big.bin":   base.Add(3 * time.Hour),
		"m.md":      base.Add(2 * time.Hour),
		"small.txt": base.Add(4 * time.Hour),
	}
	for name, ts := range times {
		if err := os.Chtimes(filepath.Join(root, name), ts, ts); err != nil {
			t.Fatal(err)
		}
	}

	return root
}

func listNames(t *testing.T, opts model.ListOptions) []string {
	t.Helper()

	items, err := List(opts)
	if err != nil {
		t.Fatalf("List(%+v): %v", opts, err)
	}

	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestListSortByName(t *testing.T) {
	listFixture(t)

	tests := []struct {
		name  string
		order model.SortOrder
		want  []string
	}{
		{
			name:  "ascending with directories first",
			order: model.SortAsc,
			// directories first (adir, zeta), then files by name (big.bin, m.md, small.txt)
			want: []string{"adir", "zeta", "big.bin", "m.md", "small.txt"},
		},
		{
			name:  "descending keeps directories first",
			order: model.SortDesc,
			// directories first (zeta, adir), then files by name descending
			want: []string{"zeta", "adir", "small.txt", "m.md", "big.bin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := listNames(t, model.ListOptions{SortBy: model.SortByName, SortOrder: tt.order})
			if !equalStrings(got, tt.want) {
				t.Errorf("order = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestListSortBySize(t *testing.T) {
	listFixture(t)

	tests := []struct {
		name  string
		order model.SortOrder
		want  []string
	}{
		{
			name:  "ascending with directories first",
			order: model.SortAsc,
			// dirs first (adir, zeta), then files smallest to largest
			want: []string{"adir", "zeta", "small.txt", "m.md", "big.bin"},
		},
		{
			name:  "descending with directories first",
			order: model.SortDesc,
			// dirs first (adir, zeta kept in name order as a tiebreak), files largest to smallest
			want: []string{"adir", "zeta", "big.bin", "m.md", "small.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := listNames(t, model.ListOptions{SortBy: model.SortBySize, SortOrder: tt.order})
			if !equalStrings(got, tt.want) {
				t.Errorf("order = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestListSortByModified(t *testing.T) {
	listFixture(t)

	// mtimes: adir=+5h, small.txt=+4h, big.bin=+3h, m.md=+2h, zeta=+1h
	tests := []struct {
		name  string
		order model.SortOrder
		want  []string
	}{
		{
			name:  "oldest first keeps directories first",
			order: model.SortAsc,
			// dirs first (zeta older than adir), then files oldest to newest
			want: []string{"zeta", "adir", "m.md", "big.bin", "small.txt"},
		},
		{
			name:  "newest first keeps directories first",
			order: model.SortDesc,
			// dirs first (adir newer than zeta), then files newest to oldest
			want: []string{"adir", "zeta", "small.txt", "big.bin", "m.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := listNames(t, model.ListOptions{SortBy: model.SortByModified, SortOrder: tt.order})
			if !equalStrings(got, tt.want) {
				t.Errorf("order = %v, want %v", got, tt.want)
			}
		})
	}
}

// Directories must always sort before files, regardless of sort key or order.
func TestListAlwaysPlacesDirectoriesFirst(t *testing.T) {
	listFixture(t)

	combos := []model.ListOptions{
		{SortBy: model.SortByName, SortOrder: model.SortAsc},
		{SortBy: model.SortByName, SortOrder: model.SortDesc},
		{SortBy: model.SortBySize, SortOrder: model.SortAsc},
		{SortBy: model.SortBySize, SortOrder: model.SortDesc},
		{SortBy: model.SortByModified, SortOrder: model.SortAsc},
		{SortBy: model.SortByModified, SortOrder: model.SortDesc},
	}

	for _, opts := range combos {
		items, err := List(opts)
		if err != nil {
			t.Fatalf("List(%+v): %v", opts, err)
		}

		seenFile := false
		for _, item := range items {
			if item.IsDir {
				if seenFile {
					t.Fatalf("List(%+v): directory %q appears after a file", opts, item.Name)
				}
				continue
			}
			seenFile = true
		}
	}
}

// Equal sort keys fall back to a case-insensitive ascending name comparison, so
// the ordering is deterministic rather than dependent on ReadDir order.
func TestListTiesBreakByNameAscending(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	// Same size, and force the same mtime, so only the name tiebreak applies.
	for _, name := range []string{"bravo.txt", "Alpha.txt", "charlie.txt"} {
		writeFile(t, filepath.Join(root, name), "aaa")
	}

	stamp := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)
	for _, name := range []string{"bravo.txt", "Alpha.txt", "charlie.txt"} {
		if err := os.Chtimes(filepath.Join(root, name), stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}

	want := []string{"Alpha.txt", "bravo.txt", "charlie.txt"}

	for _, by := range []model.SortBy{model.SortByName, model.SortBySize, model.SortByModified} {
		got := listNames(t, model.ListOptions{SortBy: by, SortOrder: model.SortAsc})
		if !equalStrings(got, want) {
			t.Errorf("SortBy=%v asc name tiebreak: got %v, want %v", by, got, want)
		}
	}
}

// Unknown or empty sort options normalise to name ascending.
func TestListNormalisesSortOptions(t *testing.T) {
	listFixture(t)

	want := []string{"adir", "zeta", "big.bin", "m.md", "small.txt"}

	got := listNames(t, model.ListOptions{})
	if !equalStrings(got, want) {
		t.Errorf("default options: got %v, want %v", got, want)
	}

	got = listNames(t, model.ListOptions{SortBy: model.SortBy("bogus"), SortOrder: model.SortOrder("bogus")})
	if !equalStrings(got, want) {
		t.Errorf("bogus options: got %v, want %v", got, want)
	}
}
