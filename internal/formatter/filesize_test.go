package formatter

import "testing"

// TestFormatSize covers the unit boundaries of FormatSize. The implementation
// switches on binary units (1 KB = 1024, 1 MB = 1024 KB, 1 GB = 1024 MB), prints
// two decimals for KB and above and a plain integer with a "B" suffix below
// 1 KB. These expectations are derived from the current implementation; they
// document behaviour rather than propose a new one.
func TestFormatSize(t *testing.T) {
	tests := []struct {
		name string
		size int64
		want string
	}{
		{"zero", 0, "0 B"},
		{"one byte", 1, "1 B"},
		{"below one KB", 512, "512 B"},
		{"just below one KB", 1023, "1023 B"},
		{"exactly one KB", 1024, "1.00 KB"},
		{"one byte over one KB", 1025, "1.00 KB"},
		{"one and a half KB", 1536, "1.50 KB"},
		{"just below one MB", 1024*1024 - 1, "1024.00 KB"},
		{"exactly one MB", 1024 * 1024, "1.00 MB"},
		{"one byte over one MB", 1024*1024 + 1, "1.00 MB"},
		{"one and a half MB", 1536 * 1024, "1.50 MB"},
		{"just below one GB", 1024*1024*1024 - 1, "1024.00 MB"},
		{"exactly one GB", 1024 * 1024 * 1024, "1.00 GB"},
		{"one byte over one GB", 1024*1024*1024 + 1, "1.00 GB"},
		{"two and a half GB", 2560 * 1024 * 1024, "2.50 GB"},
		{"one TB", 1024 * 1024 * 1024 * 1024, "1024.00 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatSize(tt.size); got != tt.want {
				t.Errorf("FormatSize(%d) = %q, want %q", tt.size, got, tt.want)
			}
		})
	}
}

// FormatSize always uses binary units, never SI (1000-based) units.
func TestFormatSizeUsesBinaryUnits(t *testing.T) {
	if got := FormatSize(1000); got != "1000 B" {
		t.Errorf("FormatSize(1000) = %q, want %q (binary, not SI)", got, "1000 B")
	}
	if got := FormatSize(1000 * 1000); got != "976.56 KB" {
		t.Errorf("FormatSize(1000000) = %q, want %q", got, "976.56 KB")
	}
}
