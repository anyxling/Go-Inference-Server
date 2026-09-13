package main

import (
	"log"
	"net/http"
	"time"
)

func main() {

	mux := http.NewServeMux()

	healthHandler := func(w http.ResponseWriter, _ *http.Request) {

	}

	server := http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	mux.HandleFunc("GET /health", healthHandler)
	log.Fatal(server.ListenAndServe())
}
