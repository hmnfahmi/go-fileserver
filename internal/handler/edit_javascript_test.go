package handler

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestEditJavaScriptBehavior runs a small Node-based DOM shim against the real
// edit.js to verify the interaction that Go tests cannot observe: unsaved-change
// detection, the saving state, duplicate-submit prevention, the beforeunload
// warning and the pageshow reset.
//
// It reuses the project's lightweight harness pattern and skips cleanly when
// Node is unavailable.
func TestEditJavaScriptBehavior(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available; skipping lightweight edit.js behaviour check")
	}

	harness := filepath.Join("testdata", "edit_dom_harness.js")
	script := filepath.Join("..", "..", "web", "static", "js", "edit.js")

	out, err := exec.Command(node, harness, script).CombinedOutput()
	if err != nil {
		t.Fatalf("edit.js behaviour check failed: %v\n%s", err, out)
	}

	t.Logf("edit.js behaviour check passed:\n%s", out)
}
