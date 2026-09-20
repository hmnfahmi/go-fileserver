package service

import (
	"log"
	"os"
	"simple-http-fileserver-go/internal/config"
)

func Initialize() error {
	return ensureDirectory(config.SharedPath)
}

func ensureDirectory(path string) error {
	info, err := os.Stat(path)

	if os.IsNotExist(err) {
		log.Printf("Directory '%s' not found, creating... \n", path)
		return os.MkdirAll(path, 0755)
	}

	if err != nil {
		return err
	}

	if !info.IsDir() {
		return os.ErrInvalid
	}

	return nil
}
