package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

const maxOutputTokensLimit = 1024 // move to config later

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
		if inferenceReq.Model == "" {
			http.Error(w, "model cannot be empty", http.StatusBadRequest)
			return
		}
		if inferenceReq.Prompt == "" {
			http.Error(w, "prompt cannot be empty", http.StatusBadRequest)
			return
		}
		if inferenceReq.Temperature > 2 || inferenceReq.Temperature < 0 {
			http.Error(w, "temperature must be between 0 and 2", http.StatusBadRequest)
			return
		}
		if inferenceReq.MaxOutputTokens == 0 {
			inferenceReq.MaxOutputTokens = 256
		}
		if inferenceReq.MaxOutputTokens > maxOutputTokensLimit || inferenceReq.MaxOutputTokens < 1 {
			http.Error(w, fmt.Sprintf("max_output_tokens should be within 1-%d", maxOutputTokensLimit), http.StatusBadRequest)
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
