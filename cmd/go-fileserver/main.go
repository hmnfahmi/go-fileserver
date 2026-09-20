package main

import (
	"go-fileserver/internal/config"
	"go-fileserver/internal/handler"
	"go-fileserver/internal/netinfo"
	"go-fileserver/internal/service"
	simpleweb "go-fileserver/web"
	"log"
	"net/http"
)

func main() {
	if err := config.Init(); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	if err := service.Initialize(); err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/", handler.Browse)
	mux.HandleFunc("/view", handler.View)
	mux.HandleFunc("/download", handler.Download)
	mux.HandleFunc("/upload", handler.Upload)
	mux.HandleFunc("/delete", handler.Delete)
	mux.HandleFunc("/rename", handler.Rename)

	staticFS, err := simpleweb.StaticFS()
	if err != nil {
		log.Fatalf("Failed to load embedded static assets: %v", err)
	}

	mux.Handle("/static/",
		http.StripPrefix("/static/",
			http.FileServer(http.FS(staticFS)),
		),
	)

	log.Println("HTTP File Server started")
	log.Printf("Config:      %s", config.ConfigPath)
	log.Printf("Port:        %s", config.Port)
	log.Printf("Shared path: %s", config.SharedPath)

	printAddresses()

	// The server binds to all interfaces so any reachable address works. The
	// addresses above are informational, not a binding restriction.
	log.Fatal(http.ListenAndServe("0.0.0.0:"+config.Port, mux))
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
