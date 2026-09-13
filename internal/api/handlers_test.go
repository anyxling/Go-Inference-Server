package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	HealthHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestInferenceHandler(t *testing.T) {
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
		{name: "body over 1 MB", body: `{"model":"llm","prompt":"`+prompt+`","temperature":2}`, wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/inference", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			InferenceHandler(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}
