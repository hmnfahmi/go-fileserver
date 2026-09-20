package service

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Raw filesystem errors that escape the sentinel model must still produce a
// controlled, client-safe message (and never err.Error()).
func TestPublicMessageClassifiesOSErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"access denied", ErrAccessDenied, "Access denied"},
		{"not found", ErrNotFound, "File or folder not found"},
		{"raw not exist", os.ErrNotExist, "File or folder not found"},
		{"wrapped not exist", fmt.Errorf("stat: %w", os.ErrNotExist), "File or folder not found"},
		{"raw permission", os.ErrPermission, "Access denied"},
		{"wrapped permission", fmt.Errorf("open: %w", os.ErrPermission), "Access denied"},
		{"raw exist", os.ErrExist, "File already exists"},
		{"wrapped exist", fmt.Errorf("rename: %w", os.ErrExist), "File already exists"},
		{"generic", fmt.Errorf("boom"), "Operation failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PublicMessage(tc.err); got != tc.want {
				t.Errorf("PublicMessage(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestPublicMessageDoesNotLeakPaths(t *testing.T) {
	pathErr := &os.PathError{Op: "open", Path: `D:\secrets\file.txt`, Err: os.ErrNotExist}

	msg := PublicMessage(pathErr)
	if strings.Contains(msg, "secrets") || strings.Contains(msg, `D:\`) {
		t.Errorf("PublicMessage leaked an internal path: %q", msg)
	}

	permErr := &os.PathError{Op: "open", Path: "/root/private", Err: os.ErrPermission}
	msg = PublicMessage(permErr)
	if strings.Contains(msg, "private") || strings.Contains(msg, "/root") {
		t.Errorf("PublicMessage leaked an internal path: %q", msg)
	}
}
