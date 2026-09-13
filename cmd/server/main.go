package main

import (
	"log"
	"net/http"
	"time"

	"github.com/monikaliu/go-inference-server/internal/api"
)

func main() {
	mux := http.NewServeMux()

	server := http.Server{
		Addr:              ":8080",
		Handler:           api.RequestID(mux),
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	mux.HandleFunc("GET /health", api.HealthHandler)
	mux.HandleFunc("POST /v1/inference", api.InferenceHandler)
	log.Fatal(server.ListenAndServe())
}
