package main

import (
	"context"
	"errors"
	"fmt"
	"go-fileserver/internal/config"
	"go-fileserver/internal/handler"
	"go-fileserver/internal/netinfo"
	"go-fileserver/internal/service"
	simpleweb "go-fileserver/web"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"
)

// Server hardening constants. They are deliberately implementation details and
// are not exposed through config.yaml.
const (
	// readHeaderTimeout bounds how long a client may take to send request
	// headers, protecting the server from slow-header (Slowloris-style) clients.
	readHeaderTimeout = 10 * time.Second
	// idleTimeout bounds how long an idle keep-alive connection is kept open.
	idleTimeout = 120 * time.Second
	// maxHeaderBytes bounds the size of request headers.
	maxHeaderBytes = 1 << 20 // 1 MB
	// shutdownTimeout bounds how long graceful shutdown waits for in-flight
	// requests to finish before the process exits.
	shutdownTimeout = 5 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// run executes the application lifecycle: load configuration, initialise the
// shared directory, build the HTTP server and serve until a shutdown signal is
// received. Returning an error lets main decide how to exit the process, which
// keeps the lifecycle itself testable.
//
// Note on timeouts: ReadTimeout and WriteTimeout are intentionally left at zero
// because uploads may be up to config.MaxUploadSize (100 MB by default),
// downloads may be large, and previews are already bounded by
// config.MaxPreviewSize. A fixed read/write deadline would abort legitimate
// large transfers. Header size is bounded by maxHeaderBytes and the upload body
// by http.MaxBytesReader in the upload handler.
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := config.Init(); err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	if err := service.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize application: %w", err)
	}

	mux, err := buildMux()
	if err != nil {
		return fmt.Errorf("failed to load embedded static assets: %w", err)
	}

	// Bind before serving so a port conflict is reported as a startup error
	// rather than being hidden inside the serving goroutine.
	addr := "0.0.0.0:" + config.Port
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", addr, err)
	}

	logStartup()

	return serve(ctx, ln, mux)
}

// serve runs the HTTP server until ctx is cancelled, then shuts it down
// gracefully. It is split out from run so the lifecycle can be tested without
// sending real process signals. Passing the listener in also lets tests bind an
// ephemeral port (127.0.0.1:0).
func serve(ctx context.Context, ln net.Listener, handler http.Handler) error {
	server := newServer(ln.Addr().String(), handler)

	errCh := make(chan error, 1)

	go func() {
		errCh <- server.Serve(ln)
	}()

	select {
	case err := <-errCh:
		// Serve only returns before shutdown when startup fails.
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		log.Println()
		log.Println("Shutdown signal received, waiting for active requests...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Graceful shutdown did not complete: %v", err)
			return err
		}

		log.Println("Server stopped cleanly.")
		return nil
	}
}

// newServer constructs the HTTP server with the project's hardening defaults.
// It is a small seam so the configuration is inspectable from tests.
func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
}

// buildMux registers every route. Handler semantics are unchanged.
func buildMux() (*http.ServeMux, error) {
	staticFS, err := simpleweb.StaticFS()
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/", handler.Browse)
	mux.HandleFunc("/view", handler.View)
	mux.HandleFunc("/download", handler.Download)
	mux.HandleFunc("/zip", handler.Zip)
	mux.HandleFunc("/upload", handler.Upload)
	mux.HandleFunc("/delete", handler.Delete)
	mux.HandleFunc("/rename", handler.Rename)

	mux.Handle("/static/",
		http.StripPrefix("/static/",
			http.FileServer(http.FS(staticFS)),
		),
	)

	return mux, nil
}

// logStartup writes the informational startup banner.
func logStartup() {
	log.Println("HTTP File Server started")
	log.Printf("Config:      %s", config.ConfigPath)
	log.Printf("Port:        %s", config.Port)
	log.Printf("Shared path: %s", config.SharedPath)

	printAddresses()
}

func printAddresses() {
	addrs := netinfo.Collect()

	log.Println()
	log.Println("Available addresses:")

	if len(addrs) == 0 {
		log.Println("  (no usable IPv4 addresses found)")
		log.Printf("  http://127.0.0.1:%s", config.Port)
		return
	}

	for _, addr := range addrs {
		log.Printf("  %s", formatInterfaceLabel(addr))
		log.Printf("    %s", addr.URL(config.Port))
	}

	log.Println()
	log.Println("Use the address that another device on the same network can reach.")
}

func formatInterfaceLabel(addr netinfo.Address) string {
	switch {
	case addr.Loopback:
		return addr.Interface + " (loopback, this machine only)"
	case addr.Virtual:
		return addr.Interface + " (virtual adapter)"
	default:
		return addr.Interface
	}
}
