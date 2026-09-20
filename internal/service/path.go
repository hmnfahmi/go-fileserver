package service

import (
	"errors"
	"path/filepath"
	"simple-http-fileserver-go/internal/config"
)

func SafePath(rel string) (string, error) {
	base, err := filepath.Abs(config.SharedPath)
	if err != nil {
		return "", err
	}

	target := filepath.Join(base, rel)

	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}

	relative, err := filepath.Rel(base, target)
	if err != nil {
		return "", err
	}

	if relative == ".." || len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator) {
		return "", errors.New("access denied")
	}

	return target, nil
}
