package handler

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestUploadJavaScriptBehavior runs a small Node-based DOM shim against the real
// upload.js to verify the transient interaction state that Go tests cannot
// observe: loading activation, duplicate-submission prevention, cleanup on
// success/failure, multi-file selection and drag & drop.
//
// The project has no browser test framework and adding one would be
// disproportionate for this phase, so this reuses the existing "Go test"
// strategy and skips cleanly when Node is unavailable.
func TestUploadJavaScriptBehavior(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available; skipping lightweight upload.js behaviour check")
	}

	harness := filepath.Join("testdata", "upload_dom_harness.js")
	script := filepath.Join("..", "..", "web", "static", "js", "upload.js")

	out, err := exec.Command(node, harness, script).CombinedOutput()
	if err != nil {
		t.Fatalf("upload.js behaviour check failed: %v\n%s", err, out)
	}

	t.Logf("upload.js behaviour check passed:\n%s", out)
}
