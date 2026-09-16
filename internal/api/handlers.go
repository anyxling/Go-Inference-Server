package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/monikaliu/go-inference-server/internal/worker"
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

type inferenceResponse struct {
	Text            string `json:"text"`
	GeneratedTokens int    `json:"generated_tokens"`
}

type tokenEvent struct {
	Text string
}

type doneEvent struct {
	FinishReason    string
	GeneratedTokens int
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	err := json.NewEncoder(w).Encode(v)
	if err != nil {
		log.Printf("Write JSON response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, code string, msg string) {
	response := errorResponse{
		Code:    code,
		Message: msg,
	}
	writeJSON(w, status, response)
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
	req := worker.Request{
		Model:           inferenceReq.Model,
		Prompt:          inferenceReq.Prompt,
		MaxOutputTokens: inferenceReq.MaxOutputTokens,
		Temperature:     inferenceReq.Temperature,
	}
	ch, err := s.generator.Generate(r.Context(), req)
	if err != nil {
		log.Printf("generate: %v", err)
		writeError(w, 503, "worker_unavailable", "Inference worker is unavailable")
		return
	}
	if inferenceReq.Stream {
		s.streamTokens(w, ch)
		return
	}
	s.writeCollected(w, ch)
}

func (s *Server) writeCollected(w http.ResponseWriter, ch <-chan worker.Token) {
	var sb strings.Builder
	var count int
	for token := range ch {
		if token.Err != nil {
			log.Printf("generate token: %v", token.Err)
			writeError(w, 500, "internal_error", "Token not generated successfully")
			return
		}
		sb.WriteString(token.Text)
		count++
	}
	res := inferenceResponse{
		Text:            sb.String(),
		GeneratedTokens: count,
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) streamTokens(w http.ResponseWriter, ch <-chan worker.Token) {
	var count int
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "internal_error", "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	for token := range ch {
		if token.Err != nil {
			log.Printf("generate token: %v", token.Err)
			err := writeSSE(w, "error", errorResponse{"internal_error", "Token not generated successfully"})
			if err != nil {
				log.Printf("Error write fail: %v", err)
			}
			flusher.Flush()
			return
		}
		err := writeSSE(w, "token", tokenEvent{Text: token.Text})
		if err != nil {
			log.Printf("Token write fail: %v", err)
			return
		}
		flusher.Flush()
		count++
	}
	err := writeSSE(w, "done", doneEvent{FinishReason: "stop", GeneratedTokens: count})
	if err != nil {
		log.Printf("Done write fail: %v", err)
		return
	}
	flusher.Flush()
}
