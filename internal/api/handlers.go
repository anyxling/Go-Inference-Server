package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
)

const maxOutputTokensLimit = 1024 // move to config later
const defaultMaxOutputTokens = 256
const requestBodyLimit = 1 << 20

type inferenceRequest struct {
	Model           string  `json:"model"`
	Prompt          string  `json:"prompt"`
	MaxOutputTokens int     `json:"max_output_tokens"`
	Temperature     float64 `json:"temperature"`
	Stream          bool    `json:"stream"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code string, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	response := errorResponse{
		Code:    code,
		Message: msg,
	}
	err := json.NewEncoder(w).Encode(response)
	if err != nil {
		log.Printf("Write error response: %v", err)
	}
}

func (s *Server) HealthHandler(w http.ResponseWriter, _ *http.Request) {}

func (s *Server) InferenceHandler(w http.ResponseWriter, r *http.Request) {
	var inferenceReq inferenceRequest
	maxReader := http.MaxBytesReader(w, r.Body, requestBodyLimit)
	decoder := json.NewDecoder(maxReader)
	err := decoder.Decode(&inferenceReq)
	if err != nil {
		var maxByteErr *http.MaxBytesError
		if errors.As(err, &maxByteErr) {
			writeError(w, 413, "request_too_large", fmt.Sprintf("Request cannot exceed %d bytes", requestBodyLimit))
			return
		}
		writeError(w, 400, "invalid_request", "Bad request")
		return
	}
	if inferenceReq.Model == "" {
		writeError(w, 400, "invalid_request", "model cannot be empty")
		return
	}
	if inferenceReq.Prompt == "" {
		writeError(w, 400, "invalid_request", "prompt cannot be empty")
		return
	}
	if inferenceReq.Temperature > 2 || inferenceReq.Temperature < 0 {
		writeError(w, 400, "invalid_request", "temperature must be between 0 and 2")
		return
	}
	if inferenceReq.MaxOutputTokens == 0 {
		inferenceReq.MaxOutputTokens = defaultMaxOutputTokens
	}
	if inferenceReq.MaxOutputTokens > maxOutputTokensLimit || inferenceReq.MaxOutputTokens < 1 {
		writeError(w, 400, "invalid_request", fmt.Sprintf("max_output_tokens should be within 1-%d", maxOutputTokensLimit))
		return
	}
}
