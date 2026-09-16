package main

import (
	"log"
	"net/http"
	"time"

	"github.com/monikaliu/go-inference-server/internal/api"
	"github.com/monikaliu/go-inference-server/internal/worker"
)

func main() {
	mux := http.NewServeMux()

	server := http.Server{
		Addr:              ":8080",
		Handler:           api.RequestID(mux),
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	app := api.NewServer(&worker.Fake{
		Tokens: []string{"a", "b", "c"},
		Delay:  100 * time.Millisecond,
	})

	mux.HandleFunc("GET /health", app.HealthHandler)
	mux.HandleFunc("POST /v1/inference", app.InferenceHandler)
	log.Fatal(server.ListenAndServe())
}
