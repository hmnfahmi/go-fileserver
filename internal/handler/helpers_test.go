package handler

import (
	"reflect"
	"strings"
	"testing"
)

// TestBuildBreadcrumb covers the observable structure returned for a canonical
// (CleanRel'd) logical path: a fixed "Home" root followed by one entry per path
// segment, where each entry's Path is the cumulative slash-separated prefix.
func TestBuildBreadcrumb(t *testing.T) {
	tests := []struct {
		name    string
		logical string
		want    []BreadcrumbItem
	}{
		{
			name:    "root",
			logical: "",
			want: []BreadcrumbItem{
				{Name: "Home", Path: ""},
			},
		},
		{
			name:    "one level",
			logical: "docs",
			want: []BreadcrumbItem{
				{Name: "Home", Path: ""},
				{Name: "docs", Path: "docs"},
			},
		},
		{
			name:    "multiple levels",
			logical: "docs/2026/reports",
			want: []BreadcrumbItem{
				{Name: "Home", Path: ""},
				{Name: "docs", Path: "docs"},
				{Name: "2026", Path: "docs/2026"},
				{Name: "reports", Path: "docs/2026/reports"},
			},
		},
		{
			name:    "leading and trailing slashes are ignored",
			logical: "/docs/",
			want: []BreadcrumbItem{
				{Name: "Home", Path: ""},
				{Name: "docs", Path: "docs"},
			},
		},
		{
			name:    "empty segments are skipped",
			logical: "a//b",
			want: []BreadcrumbItem{
				{Name: "Home", Path: ""},
				{Name: "a", Path: "a"},
				{Name: "b", Path: "a/b"},
			},
		},
		{
			name:    "special characters are preserved verbatim",
			logical: "a&b/c d",
			want: []BreadcrumbItem{
				{Name: "Home", Path: ""},
				{Name: "a&b", Path: "a&b"},
				{Name: "c d", Path: "a&b/c d"},
			},
		},
		{
			name:    "backslash is not a separator",
			logical: `a\b`,
			want: []BreadcrumbItem{
				{Name: "Home", Path: ""},
				{Name: `a\b`, Path: `a\b`},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildBreadcrumb(tt.logical)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildBreadcrumb(%q) = %#v, want %#v", tt.logical, got, tt.want)
			}
		})
	}
}

// TestBuildBreadcrumbAlwaysStartsWithHome documents the invariant relied on by
// the template: the first entry is always Home with an empty path.
func TestBuildBreadcrumbAlwaysStartsWithHome(t *testing.T) {
	for _, logical := range []string{"", "a", "a/b/c", "a/b/c/d/e/f"} {
		got := buildBreadcrumb(logical)
		if len(got) == 0 {
			t.Fatalf("buildBreadcrumb(%q) returned no entries", logical)
		}
		if got[0].Name != "Home" || got[0].Path != "" {
			t.Errorf("buildBreadcrumb(%q) first entry = %#v, want Home with empty path", logical, got[0])
		}
	}
}

// TestSanitizeHeaderFilename documents the current Content-Disposition
// sanitization: double quotes, carriage returns, line feeds and backslashes are
// stripped, while every other byte (including spaces, Unicode and forward
// slashes) is preserved.
func TestSanitizeHeaderFilename(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"normal ascii", "report.pdf", "report.pdf"},
		{"spaces preserved", "my report 2026.pdf", "my report 2026.pdf"},
		{"unicode preserved", "résumé-文件.txt", "résumé-文件.txt"},
		{"double quotes stripped", `a"b.txt`, "ab.txt"},
		{"backslashes stripped", `a\b\c.txt`, "abc.txt"},
		{"carriage return stripped", "a\rb.txt", "ab.txt"},
		{"line feed stripped", "a\nb.txt", "ab.txt"},
		{"crlf stripped", "a\r\nb.txt", "ab.txt"},
		{"tab is preserved", "a\tb.txt", "a\tb.txt"},
		{"path-like windows input", `..\..\etc\passwd`, "....etcpasswd"},
		{"forward slashes preserved", "/etc/passwd", "/etc/passwd"},
		{"combined dangerous characters", "bad\"\r\n\\name.txt", "badname.txt"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeHeaderFilename(tt.input); got != tt.want {
				t.Errorf("sanitizeHeaderFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// A sanitized filename must never contain the characters that would break out
// of the quoted Content-Disposition value.
func TestSanitizeHeaderFilenameRemovesHeaderBreakingCharacters(t *testing.T) {
	const input = "a\"b\\c\r\nd.txt"

	got := sanitizeHeaderFilename(input)
	for _, forbidden := range []string{`"`, `\`, "\r", "\n"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("sanitizeHeaderFilename(%q) = %q, still contains %q", input, got, forbidden)
		}
	}
}
