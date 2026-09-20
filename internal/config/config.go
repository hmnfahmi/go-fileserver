package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the name of the external configuration file. It is intentionally
// kept outside the binary so the user can edit it without rebuilding.
const FileName = "config.yaml"

// Config is the strongly typed representation of config.yaml.
type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Storage StorageConfig `yaml:"storage"`
}

type ServerConfig struct {
	// Port is kept as a string so both `port: "8088"` and `port: 8088` are
	// accepted, matching the original configuration file.
	Port string `yaml:"port"`
}

type StorageConfig struct {
	SharedPath     string `yaml:"shared_path"`
	MaxUploadSize  int64  `yaml:"max_upload_size"`
	MaxPreviewSize int64  `yaml:"max_preview_size"`
	// MaxEditSize bounds the size of a file that may be opened in the browser
	// text editor. It is intentionally independent of MaxPreviewSize and
	// MaxUploadSize: editing is limited by what a textarea can handle, not by
	// what can be previewed or uploaded.
	MaxEditSize int64 `yaml:"max_edit_size"`
}

// Values used by the rest of the application. They are populated once by Init
// so the existing service/handler packages can keep reading them.
var (
	Port           string
	SharedPath     string
	MaxUploadSize  int64
	MaxPreviewSize int64
	MaxEditSize    int64
	ConfigPath     string
)

// Default returns the built-in configuration used when a key is omitted from
// config.yaml. It is also the documented fallback.
func Default() Config {
	return Config{
		Server: ServerConfig{
			Port: "8080",
		},
		Storage: StorageConfig{
			SharedPath:     "./shared",
			MaxUploadSize:  100 * 1024 * 1024,
			MaxPreviewSize: 2 * 1024 * 1024,
			MaxEditSize:    1 * 1024 * 1024,
		},
	}
}

// Init discovers config.yaml, loads and validates it, and publishes the values
// to the package-level variables. Configuration is read once at startup; a
// change requires restarting the application.
func Init() error {
	path, err := Discover()
	if err != nil {
		return err
	}

	cfg, err := Load(path)
	if err != nil {
		return err
	}

	Port = cfg.Server.Port
	SharedPath = cfg.Storage.SharedPath
	MaxUploadSize = cfg.Storage.MaxUploadSize
	MaxPreviewSize = cfg.Storage.MaxPreviewSize
	MaxEditSize = cfg.Storage.MaxEditSize
	ConfigPath = path

	return nil
}

// Discover returns the path of the first config.yaml found next to the
// executable, then in the current working directory. The executable directory
// is checked first so a portable deployment does not depend on how the process
// was launched.
func Discover() (string, error) {
	return findConfig(searchDirs())
}

func searchDirs() []string {
	var dirs []string

	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}

	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, wd)
	}

	return dirs
}

func findConfig(dirs []string) (string, error) {
	seen := make(map[string]bool)
	var searched []string

	for _, dir := range dirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true

		path := filepath.Join(dir, FileName)
		searched = append(searched, path)

		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path, nil
		}
	}

	if len(searched) == 0 {
		return "", fmt.Errorf("could not determine where to look for %s", FileName)
	}

	return "", fmt.Errorf("%s not found (searched: %s)", FileName, strings.Join(searched, ", "))
}

// Load reads, validates and resolves the configuration file at path. Relative
// storage paths are resolved against the directory containing the config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read config file %q: %w", path, err)
	}

	cfg := Default()

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("cannot parse config file %q: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration in %q: %w", path, err)
	}

	resolved, err := resolveSharedPath(filepath.Dir(path), cfg.Storage.SharedPath)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration in %q: %w", path, err)
	}
	cfg.Storage.SharedPath = resolved

	return &cfg, nil
}

// Validate rejects obviously invalid configuration values.
func (c Config) Validate() error {
	if err := ValidatePort(c.Server.Port); err != nil {
		return err
	}

	if strings.TrimSpace(c.Storage.SharedPath) == "" {
		return errors.New("storage.shared_path must not be empty")
	}

	if c.Storage.MaxUploadSize <= 0 {
		return fmt.Errorf("storage.max_upload_size must be greater than 0 (got %d)", c.Storage.MaxUploadSize)
	}

	if c.Storage.MaxPreviewSize <= 0 {
		return fmt.Errorf("storage.max_preview_size must be greater than 0 (got %d)", c.Storage.MaxPreviewSize)
	}

	if c.Storage.MaxPreviewSize > c.Storage.MaxUploadSize {
		return fmt.Errorf(
			"storage.max_preview_size (%d) must not exceed storage.max_upload_size (%d)",
			c.Storage.MaxPreviewSize,
			c.Storage.MaxUploadSize,
		)
	}

	if c.Storage.MaxEditSize <= 0 {
		return fmt.Errorf("storage.max_edit_size must be greater than 0 (got %d)", c.Storage.MaxEditSize)
	}

	return nil
}

// ValidatePort checks that port is a usable TCP port.
func ValidatePort(port string) error {
	port = strings.TrimSpace(port)
	if port == "" {
		return errors.New("server.port must not be empty")
	}

	n, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("server.port must be a number (got %q)", port)
	}

	if n < 1 || n > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535 (got %d)", n)
	}

	return nil
}

// resolveSharedPath turns a configured shared_path into an absolute path.
// Absolute paths (POSIX, Windows drive or UNC) are used as-is; relative paths
// are resolved against baseDir, which is the directory holding config.yaml.
func resolveSharedPath(baseDir, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("storage.shared_path must not be empty")
	}

	if isAbsolutePath(raw) {
		return filepath.Clean(raw), nil
	}

	if baseDir == "" {
		return "", fmt.Errorf("cannot resolve relative shared path %q without a base directory", raw)
	}

	return filepath.Clean(filepath.Join(baseDir, raw)), nil
}

// isAbsolutePath recognises absolute paths from the host OS as well as
// Windows-style paths (drive letter and UNC) so configuration can be validated
// consistently on any platform.
func isAbsolutePath(p string) bool {
	if filepath.IsAbs(p) {
		return true
	}

	if len(p) >= 3 && isDriveLetter(p[0]) && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		return true
	}

	if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, `//`) {
		return true
	}

	return false
}

func isDriveLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
