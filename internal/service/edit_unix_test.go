//go:build unix

package service

import (
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// A FIFO (named pipe) is a non-regular file. It must be rejected by the regular
// file check before any open, so the read cannot block waiting for a writer.
func TestReadEditableFileRejectsFIFO(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	fifo := filepath.Join(root, "pipe.txt")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("cannot create FIFO: %v", err)
	}

	if _, err := ReadEditableFile("pipe.txt"); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("err = %v, want ErrNotRegular", err)
	}
}

func TestSaveEditableFileRejectsFIFO(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	fifo := filepath.Join(root, "pipe.txt")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("cannot create FIFO: %v", err)
	}

	version := FileVersion{Hash: "0000000000000000000000000000000000000000000000000000000000000000"}
	if err := SaveEditableFile("pipe.txt", []byte("x"), version); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("err = %v, want ErrNotRegular", err)
	}
}

// A write failure while the temporary file is being filled must be reported and
// must leave the target untouched. RLIMIT_FSIZE makes the write fail
// deterministically with EFBIG; SIGXFSZ is ignored so the process survives.
func TestSaveEditableFileWriteFailureLeavesTargetIntact(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	target := filepath.Join(root, "notes.txt")
	writeFile(t, target, "old")

	doc, err := ReadEditableFile("notes.txt")
	if err != nil {
		t.Fatal(err)
	}

	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
		t.Skipf("cannot read RLIMIT_FSIZE: %v", err)
	}

	limited := original
	limited.Cur = 4
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
		t.Skipf("cannot set RLIMIT_FSIZE: %v", err)
	}

	signal.Ignore(syscall.SIGXFSZ)
	defer func() {
		signal.Reset(syscall.SIGXFSZ)
		syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original)
	}()

	err = SaveEditableFile("notes.txt", []byte(strings.Repeat("x", 100)), doc.Version)
	if err == nil {
		t.Fatal("save unexpectedly succeeded despite a write failure")
	}

	if got := readString(t, target); got != "old" {
		t.Errorf("target was corrupted to %q, want old", got)
	}

	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), tempEditPrefix) {
			t.Errorf("temporary file left behind after a write failure: %s", entry.Name())
		}
	}
}
