package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type InferenceRequest struct {
	Model           string  `json:"model"`
	Prompt          string  `json:"prompt"`
	MaxOutputTokens int     `json:"max_output_tokens"`
	Temperature     float64 `json:"temperature"`
	Stream          bool    `json:"stream"`
}

func main() {

	mux := http.NewServeMux()

	healthHandler := func(w http.ResponseWriter, _ *http.Request) {}

	inferenceHandler := func(w http.ResponseWriter, r *http.Request) {
		inferenceReq := InferenceRequest{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&inferenceReq)
		if err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
	}

	server := http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("POST /v1/inference", inferenceHandler)
	log.Fatal(server.ListenAndServe())
}
