package main

import (
	"log"
	"net"
	"net/http"
	"simple-http-fileserver-go/internal/config"
	"simple-http-fileserver-go/internal/handler"
	"simple-http-fileserver-go/internal/service"
)

func main() {
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

	mux.Handle("/static/",
		http.StripPrefix("/static/",
			http.FileServer(http.Dir("web/static")),
		),
	)

	log.Println("Simple HTTP File Server")
	log.Println("Local:")
	log.Println("http://localhost:" + config.Port)

	printLANaddress()

	log.Println("Hit ke IP yang sesuai dengan ip address PC")

	// log.Fatal(http.ListenAndServe(":"+config.Port, mux))
	log.Fatal(http.ListenAndServe("0.0.0.0:"+config.Port, mux))

}

func printLANaddress() {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return
	}

	log.Println("LAN:")

	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)

		if !ok {
			continue
		}

		if ipnet.IP.IsLoopback() {
			continue
		}

		if ipnet.IP.To4() == nil {
			continue
		}

		log.Println("http://" + ipnet.IP.String() + ":" + config.Port)
	}
}
