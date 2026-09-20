package service

import (
	"fmt"
	"go-fileserver/internal/config"
	"log"
	"os"
)

func Initialize() error {
	return ensureDirectory(config.SharedPath)
}

func ensureDirectory(path string) error {
	info, err := os.Stat(path)

	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("shared path %q exists but is not a directory", path)
		}

		return nil
	}

	if !os.IsNotExist(err) {
		return fmt.Errorf("cannot access shared path %q: %w", path, err)
	}

	log.Printf("Directory '%s' not found, creating... \n", path)

	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("cannot create shared directory %q: %w", path, err)
	}

	return nil
}
