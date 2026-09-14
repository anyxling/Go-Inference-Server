package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monikaliu/go-inference-server/internal/worker"
)

func TestHealthHandler(t *testing.T) {
	server := NewServer(&worker.Fake{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.HealthHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestInferenceHandler(t *testing.T) {
	server := NewServer(&worker.Fake{})
	prompt := strings.Repeat("a", 1<<20)
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "valid request", body: `{"model":"llm","prompt":"hi"}`, wantStatus: http.StatusOK},
		{name: "malformed JSON", body: `{"model":"llm","prompt":"hi"`, wantStatus: http.StatusBadRequest},
		{name: "empty model", body: `{"model":"","prompt":"hi"}`, wantStatus: http.StatusBadRequest},
		{name: "missing model", body: `{"prompt":"hi"}`, wantStatus: http.StatusBadRequest},
		{name: "empty prompt", body: `{"model":"llm","prompt":""}`, wantStatus: http.StatusBadRequest},
		{name: "temperature too high", body: `{"model":"llm","prompt":"hi","temperature":5}`, wantStatus: http.StatusBadRequest},
		{name: "temperature negative", body: `{"model":"llm","prompt":"hi","temperature":-1}`, wantStatus: http.StatusBadRequest},
		{name: "tokens too high", body: `{"model":"llm","prompt":"hi","temperature":2,"max_output_tokens":5000}`, wantStatus: http.StatusBadRequest},
		{name: "tokens negative", body: `{"model":"llm","prompt":"hi","temperature":2,"max_output_tokens":-5}`, wantStatus: http.StatusBadRequest},
		{name: "tokens omitted", body: `{"model":"llm","prompt":"hi","temperature":2}`, wantStatus: http.StatusOK},
		{name: "body over 1 MB", body: `{"model":"llm","prompt":"` + prompt + `","temperature":2}`, wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/inference", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			server.InferenceHandler(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

func TestInferenceHandlerWorkerUnavailable(t *testing.T) {
	server := NewServer(&worker.Fake{Err: worker.ErrFakeFailure})
	req := httptest.NewRequest(http.MethodPost, "/v1/inference", strings.NewReader(`{"model":"llm","prompt":"hi"}`))
	rec := httptest.NewRecorder()

	server.InferenceHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("got %v, want %v", rec.Code, http.StatusServiceUnavailable)
	}

	var got errorResponse
	err := json.NewDecoder(rec.Body).Decode(&got)
	if err != nil {
		t.Fatalf("Should not get error but got %v", err)
	}

	if got.Code != "worker_unavailable" {
		t.Errorf("got %q, want %q", got.Code, "worker_unavailable")
	}
}

func TestInferenceHandlerInternalError(t *testing.T) {
	server := NewServer(&worker.Fake{
		Tokens:    []string{"a", "b", "c"},
		FailAfter: 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/inference", strings.NewReader(`{"model":"llm","prompt":"hi"}`))
	rec := httptest.NewRecorder()

	server.InferenceHandler(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("got %v, want %v", rec.Code, http.StatusInternalServerError)
	}

	var got errorResponse
	err := json.NewDecoder(rec.Body).Decode(&got)
	if err != nil {
		t.Fatalf("Should not get error but got %v", err)
	}

	if got.Code != "internal_error" {
		t.Errorf("got %q, want %q", got.Code, "internal_error")
	}
}
