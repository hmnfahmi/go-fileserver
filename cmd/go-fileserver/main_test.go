package main

import (
	"context"
	"go-fileserver/internal/config"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withSharedRoot points the configuration globals at a temporary directory for
// the duration of the test. config.SharedPath and the size limits are package
// globals, so tests using this must not run in parallel.
func withSharedRoot(t *testing.T, dir string) {
	t.Helper()

	originalPath := config.SharedPath
	originalPreview := config.MaxPreviewSize
	originalUpload := config.MaxUploadSize

	config.SharedPath = dir
	config.MaxPreviewSize = 1024 * 1024
	config.MaxUploadSize = 1024 * 1024

	t.Cleanup(func() {
		config.SharedPath = originalPath
		config.MaxPreviewSize = originalPreview
		config.MaxUploadSize = originalUpload
	})
}

// TestNewServerHardening inspects the server that newServer actually builds, so
// a change that stops assigning a hardening setting fails the test.
func TestNewServerHardening(t *testing.T) {
	handler := http.NewServeMux()
	srv := newServer("127.0.0.1:0", handler)

	if srv.Handler == nil {
		t.Error("Handler is nil")
	}
	if srv.ReadHeaderTimeout != 10*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 10s", srv.ReadHeaderTimeout)
	}
	if srv.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v, want 120s", srv.IdleTimeout)
	}
	if srv.MaxHeaderBytes != 1<<20 {
		t.Errorf("MaxHeaderBytes = %d, want %d", srv.MaxHeaderBytes, 1<<20)
	}

	// ReadTimeout and WriteTimeout must stay unset so large uploads and
	// downloads are not aborted mid-transfer.
	if srv.ReadTimeout != 0 {
		t.Errorf("ReadTimeout = %v, want 0", srv.ReadTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want 0", srv.WriteTimeout)
	}
}

// TestServeAcceptsRequestAndShutsDownCleanly verifies the basic lifecycle:
// start, serve a request, cancel, and return nil (ErrServerClosed is not an
// application failure).
func TestServeAcceptsRequestAndShutsDownCleanly(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, "ok")
		}))
	}()

	resp, err := http.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("serve returned %v, want nil after graceful shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after shutdown")
	}
}

// TestServeDrainsActiveRequestOnShutdown proves graceful shutdown waits for an
// in-flight request instead of cutting it off.
func TestServeDrainsActiveRequestOnShutdown(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-release
			io.WriteString(w, "finished")
		}))
	}()

	respCh := make(chan *http.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/")
		if err != nil {
			errCh <- err
			return
		}
		respCh <- resp
	}()

	select {
	case <-started:
	case err := <-errCh:
		t.Fatalf("request failed before starting: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the handler")
	}

	// The request is now in flight. Ask the server to shut down. Graceful
	// shutdown must wait for it, so serve must not return yet.
	cancel()

	select {
	case err := <-serveErr:
		t.Fatalf("serve returned (err=%v) while a request was still in flight", err)
	case <-time.After(300 * time.Millisecond):
		// Still waiting for the active request: this is the behaviour under test.
	}

	// Now let the handler finish. The response must still be delivered.
	close(release)

	select {
	case resp := <-respCh:
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
		if string(body) != "finished" {
			t.Errorf("body = %q, want %q", body, "finished")
		}
	case err := <-errCh:
		t.Fatalf("in-flight request was cut off: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request did not complete during shutdown")
	}

	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatalf("serve returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after draining the request")
	}
}

// TestServeReturnsUnexpectedServeError verifies that a real serving failure is
// propagated to the caller rather than being swallowed.
func TestServeReturnsUnexpectedServeError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	// Closing the listener makes Serve fail immediately with an unexpected error.
	ln.Close()

	if err := serve(context.Background(), ln, http.NewServeMux()); err == nil {
		t.Fatal("serve returned nil for a closed listener, want an error")
	}
}

// TestBuildMuxRegistersRoutes verifies every route is still wired after the
// lifecycle refactor. Handler behaviour itself is covered by the handler tests.
func TestBuildMuxRegistersRoutes(t *testing.T) {
	root := t.TempDir()
	withSharedRoot(t, root)

	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	mux, err := buildMux()
	if err != nil {
		t.Fatalf("buildMux: %v", err)
	}

	cases := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantType   string
	}{
		{"browse", http.MethodGet, "/", http.StatusOK, ""},
		{"view", http.MethodGet, "/view?path=f.txt", http.StatusOK, ""},
		{"download", http.MethodGet, "/download?path=f.txt", http.StatusOK, ""},
		{"zip", http.MethodGet, "/zip?path=", http.StatusOK, "application/zip"},
		{"zip method guard", http.MethodPost, "/zip?path=", http.StatusMethodNotAllowed, ""},
		{"static", http.MethodGet, "/static/css/reset.css", http.StatusOK, ""},
		{"upload method guard", http.MethodGet, "/upload", http.StatusMethodNotAllowed, ""},
		{"delete method guard", http.MethodGet, "/delete", http.StatusMethodNotAllowed, ""},
		{"rename method guard", http.MethodGet, "/rename", http.StatusMethodNotAllowed, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.target, nil))

			if rec.Code != tc.wantStatus {
				t.Errorf("%s %s status = %d, want %d", tc.method, tc.target, rec.Code, tc.wantStatus)
			}
			if tc.wantType != "" {
				if ct := rec.Header().Get("Content-Type"); ct != tc.wantType {
					t.Errorf("%s %s Content-Type = %q, want %q (route not wired to the ZIP handler)", tc.method, tc.target, ct, tc.wantType)
				}
			}
		})
	}

	// The static route must actually serve the embedded asset.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/css/reset.css", nil))
	if rec.Body.Len() == 0 {
		t.Error("static asset served an empty body")
	}
}
