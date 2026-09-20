package handler

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCreateJavaScriptBehavior runs a small Node-based DOM shim against the real
// create.js to verify the interaction that Go tests cannot observe: opening the
// correct native dialog, clearing/focusing the name field, closing any open
// dropdown, closing a dialog and the no-showModal fallback.
//
// It reuses the existing "Go test" strategy and skips cleanly when Node is
// unavailable.
func TestCreateJavaScriptBehavior(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available; skipping lightweight create.js behaviour check")
	}

	harness := filepath.Join("testdata", "create_dom_harness.js")
	script := filepath.Join("..", "..", "web", "static", "js", "create.js")

	out, err := exec.Command(node, harness, script).CombinedOutput()
	if err != nil {
		t.Fatalf("create.js behaviour check failed: %v\n%s", err, out)
	}

	t.Logf("create.js behaviour check passed:\n%s", out)
}
