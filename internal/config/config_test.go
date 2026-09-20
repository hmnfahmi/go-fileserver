package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()

	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("cannot write test config: %v", err)
	}

	return path
}

func TestDefaultIsValid(t *testing.T) {
	cfg := Default()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid, got: %v", err)
	}

	if cfg.Server.Port != "8080" {
		t.Errorf("default port = %q, want %q", cfg.Server.Port, "8080")
	}
	if cfg.Storage.SharedPath != "./shared" {
		t.Errorf("default shared path = %q, want %q", cfg.Storage.SharedPath, "./shared")
	}
	if cfg.Storage.MaxUploadSize != 100*1024*1024 {
		t.Errorf("default upload size = %d", cfg.Storage.MaxUploadSize)
	}
	if cfg.Storage.MaxPreviewSize != 2*1024*1024 {
		t.Errorf("default preview size = %d", cfg.Storage.MaxPreviewSize)
	}
	if cfg.Storage.MaxEditSize != 1*1024*1024 {
		t.Errorf("default edit size = %d, want %d", cfg.Storage.MaxEditSize, 1*1024*1024)
	}
}

func TestValidatePort(t *testing.T) {
	tests := []struct {
		name    string
		port    string
		wantErr bool
	}{
		{"valid", "8088", false},
		{"valid lower bound", "1", false},
		{"valid upper bound", "65535", false},
		{"quoted form", " 8080 ", false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"not a number", "http", true},
		{"float", "80.5", true},
		{"zero", "0", true},
		{"negative", "-1", true},
		{"too large", "65536", true},
		{"way too large", "99999", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePort(tt.port)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidatePort(%q) = nil, want error", tt.port)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidatePort(%q) = %v, want nil", tt.port, err)
			}
		})
	}
}

func TestValidateRejectsInvalidStorage(t *testing.T) {
	base := Default()

	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"empty shared path", func(c *Config) { c.Storage.SharedPath = "" }},
		{"blank shared path", func(c *Config) { c.Storage.SharedPath = "   " }},
		{"zero upload size", func(c *Config) { c.Storage.MaxUploadSize = 0 }},
		{"negative upload size", func(c *Config) { c.Storage.MaxUploadSize = -1 }},
		{"zero preview size", func(c *Config) { c.Storage.MaxPreviewSize = 0 }},
		{"negative preview size", func(c *Config) { c.Storage.MaxPreviewSize = -1 }},
		{"preview larger than upload", func(c *Config) {
			c.Storage.MaxUploadSize = 1024
			c.Storage.MaxPreviewSize = 2048
		}},
		{"zero edit size", func(c *Config) { c.Storage.MaxEditSize = 0 }},
		{"negative edit size", func(c *Config) { c.Storage.MaxEditSize = -1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			tt.mutate(&cfg)

			if err := cfg.Validate(); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestValidateAcceptsPreviewEqualToUpload(t *testing.T) {
	cfg := Default()
	cfg.Storage.MaxUploadSize = 1024
	cfg.Storage.MaxPreviewSize = 1024

	if err := cfg.Validate(); err != nil {
		t.Fatalf("preview equal to upload should be valid, got: %v", err)
	}
}

func TestLoadParsesMaxEditSize(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "storage:\n  max_edit_size: 4096\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.MaxEditSize != 4096 {
		t.Errorf("edit size = %d, want 4096", cfg.Storage.MaxEditSize)
	}
}

// The edit limit is independent of the preview and upload limits: a larger edit
// size is valid even when the other two are small.
func TestValidateAcceptsEditSizeIndependentOfOtherLimits(t *testing.T) {
	cfg := Default()
	cfg.Storage.MaxUploadSize = 1024
	cfg.Storage.MaxPreviewSize = 1024
	cfg.Storage.MaxEditSize = 8192

	if err := cfg.Validate(); err != nil {
		t.Fatalf("edit size independent of other limits should be valid, got: %v", err)
	}
}

func TestLoadParsesQuotedAndNumericPort(t *testing.T) {
	dir := t.TempDir()

	quoted := writeConfig(t, dir, "server:\n  port: \"9090\"\n")
	cfg, err := Load(quoted)
	if err != nil {
		t.Fatalf("Load quoted port: %v", err)
	}
	if cfg.Server.Port != "9090" {
		t.Errorf("quoted port = %q, want %q", cfg.Server.Port, "9090")
	}

	numeric := writeConfig(t, dir, "server:\n  port: 9091\n")
	cfg, err = Load(numeric)
	if err != nil {
		t.Fatalf("Load numeric port: %v", err)
	}
	if cfg.Server.Port != "9091" {
		t.Errorf("numeric port = %q, want %q", cfg.Server.Port, "9091")
	}
}

func TestLoadAppliesDefaultsForMissingKeys(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "server:\n  port: \"8088\"\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Storage.MaxUploadSize != Default().Storage.MaxUploadSize {
		t.Errorf("upload size = %d, want default", cfg.Storage.MaxUploadSize)
	}
	if cfg.Storage.MaxPreviewSize != Default().Storage.MaxPreviewSize {
		t.Errorf("preview size = %d, want default", cfg.Storage.MaxPreviewSize)
	}
	if cfg.Storage.MaxEditSize != Default().Storage.MaxEditSize {
		t.Errorf("edit size = %d, want default", cfg.Storage.MaxEditSize)
	}
	if cfg.Storage.SharedPath == "" {
		t.Error("shared path should fall back to the default")
	}
}

func TestLoadAppliesDefaultsWithinPartialSections(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "server:\n  port: \"8088\"\n\nstorage:\n  shared_path: \"./data\"\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	defaults := Default()

	if cfg.Storage.MaxUploadSize != defaults.Storage.MaxUploadSize {
		t.Errorf("upload size = %d, want default %d", cfg.Storage.MaxUploadSize, defaults.Storage.MaxUploadSize)
	}
	if cfg.Storage.MaxPreviewSize != defaults.Storage.MaxPreviewSize {
		t.Errorf("preview size = %d, want default %d", cfg.Storage.MaxPreviewSize, defaults.Storage.MaxPreviewSize)
	}
	if cfg.Storage.SharedPath != filepath.Join(dir, "data") {
		t.Errorf("shared path = %q, want %q", cfg.Storage.SharedPath, filepath.Join(dir, "data"))
	}
}

func TestLoadMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "server:\n  port: [unterminated\n")

	if _, err := Load(path); err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestLoadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	if _, err := Load(path); err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestLoadInvalidValueReportsError(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "server:\n  port: \"70000\"\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for out-of-range port")
	}
	if !strings.Contains(err.Error(), "server.port") {
		t.Errorf("error should mention server.port, got: %v", err)
	}
}

func TestResolveSharedPathRelative(t *testing.T) {
	base := t.TempDir()

	got, err := resolveSharedPath(base, "./shared")
	if err != nil {
		t.Fatalf("resolveSharedPath: %v", err)
	}

	want := filepath.Join(base, "shared")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if !filepath.IsAbs(got) {
		t.Errorf("resolved path should be absolute, got %q", got)
	}
}

func TestResolveSharedPathRelativeDoesNotDependOnWorkingDir(t *testing.T) {
	base := t.TempDir()
	other := t.TempDir()

	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(other); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	got, err := resolveSharedPath(base, "shared")
	if err != nil {
		t.Fatalf("resolveSharedPath: %v", err)
	}

	if !strings.HasPrefix(got, base) {
		t.Errorf("relative path resolved against working dir: got %q, want prefix %q", got, base)
	}
}

func TestResolveSharedPathAbsolute(t *testing.T) {
	base := t.TempDir()

	// On non-Windows hosts filepath.IsAbs only accepts POSIX or Windows paths
	// depending on the build target, so test the platform-native absolute case
	// and cover Windows-style paths through isAbsolutePath below.
	abs := filepath.Join(string(filepath.Separator), "var", "data", "shared")
	got, err := resolveSharedPath(base, abs)
	if err != nil {
		t.Fatalf("resolveSharedPath: %v", err)
	}
	if got != filepath.Clean(abs) {
		t.Errorf("got %q, want %q", got, filepath.Clean(abs))
	}
}

func TestIsAbsolutePathWindowsStyle(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{`D:\MyFiles\Shared`, true},
		{`C:/SharedFiles`, true},
		{`d:\shared`, true},
		{`\\server\share`, true},
		{`relative/shared`, false},
		{`./shared`, false},
		{`shared`, false},
		{`..\shared`, false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isAbsolutePath(tt.path); got != tt.want {
				t.Errorf("isAbsolutePath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestResolveSharedPathWindowsStyleIsPreserved(t *testing.T) {
	// Conceptually this is a shared path on another drive. On Linux the path is
	// not platform-native absolute, but the cross-drive intent must be kept
	// intact rather than silently joined onto the config directory.
	base := t.TempDir()
	got, err := resolveSharedPath(base, `D:\MyFiles\Shared`)
	if err != nil {
		t.Fatalf("resolveSharedPath: %v", err)
	}

	if strings.HasPrefix(got, base) {
		t.Errorf("windows-style absolute path was resolved relative to base: %q", got)
	}
	if !strings.HasPrefix(got, `D:\`) && !strings.HasPrefix(got, "D:") {
		t.Errorf("expected the drive path to be preserved, got %q", got)
	}
}

func TestResolveSharedPathEmpty(t *testing.T) {
	if _, err := resolveSharedPath(t.TempDir(), "   "); err == nil {
		t.Fatal("expected error for empty shared path")
	}
}

func TestFindConfigPrefersFirstDirectory(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()

	writeConfig(t, first, "server:\n  port: \"1111\"\n")
	writeConfig(t, second, "server:\n  port: \"2222\"\n")

	got, err := findConfig([]string{first, second})
	if err != nil {
		t.Fatalf("findConfig: %v", err)
	}
	if got != filepath.Join(first, FileName) {
		t.Errorf("got %q, want config in the first directory", got)
	}
}

func TestFindConfigFallsBackToSecondDirectory(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()

	writeConfig(t, second, "server:\n  port: \"2222\"\n")

	got, err := findConfig([]string{first, second})
	if err != nil {
		t.Fatalf("findConfig: %v", err)
	}
	if got != filepath.Join(second, FileName) {
		t.Errorf("got %q, want config in the second directory", got)
	}
}

func TestFindConfigMissingReportsSearchedPaths(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()

	_, err := findConfig([]string{first, second})
	if err == nil {
		t.Fatal("expected error when config.yaml is absent")
	}

	msg := err.Error()
	if !strings.Contains(msg, FileName) {
		t.Errorf("error should mention %s, got: %v", FileName, err)
	}
	if !strings.Contains(msg, first) || !strings.Contains(msg, second) {
		t.Errorf("error should list searched directories, got: %v", err)
	}
}

func TestFindConfigDeduplicatesDirectories(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "server:\n  port: \"8088\"\n")

	got, err := findConfig([]string{dir, dir, ""})
	if err != nil {
		t.Fatalf("findConfig: %v", err)
	}
	if got != filepath.Join(dir, FileName) {
		t.Errorf("got %q", got)
	}
}

func TestSearchDirsIncludesExecutableDirectory(t *testing.T) {
	dirs := searchDirs()

	exe, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable unavailable: %v", err)
	}

	exeDir := filepath.Dir(exe)
	if len(dirs) == 0 || dirs[0] != exeDir {
		t.Errorf("searchDirs()[0] = %v, want executable directory %q", dirs, exeDir)
	}
}

func TestLoadConfigFromDirectoryOnOtherDriveConceptually(t *testing.T) {
	// An absolute path outside of the temporary directory simulates an external
	// drive holding both config.yaml and the shared folder.
	if runtime.GOOS == "windows" {
		t.Skip("covered by the Windows build")
	}

	configDir := t.TempDir()
	path := writeConfig(t, configDir, "storage:\n  shared_path: \"/mnt/d/SharedFiles\"\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Storage.SharedPath != filepath.Clean("/mnt/d/SharedFiles") {
		t.Errorf("shared path = %q", cfg.Storage.SharedPath)
	}
}
