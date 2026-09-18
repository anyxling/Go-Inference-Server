package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monikaliu/go-inference-server/internal/worker"
)

func shortTimeouts() {
	return Timeouts{Total: 50 * time.Millisecond, FirstToken: time.Second, Idle: time.Second}
}

func TestHealthHandler(t *testing.T) {
	server := NewServer(&worker.Fake{}, DefaultTimeouts())
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.HealthHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestInferenceHandler(t *testing.T) {
	server := NewServer(&worker.Fake{}, DefaultTimeouts())
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
	server := NewServer(&worker.Fake{Err: worker.ErrFakeFailure}, DefaultTimeouts())
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
	}, DefaultTimeouts())
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

func TestInferenceHandlerSuccess(t *testing.T) {
	server := NewServer(&worker.Fake{
		Tokens: []string{"a", "b", "c"},
		Delay:  0,
	}, DefaultTimeouts())
	req := httptest.NewRequest(http.MethodPost, "/v1/inference", strings.NewReader(`{"model":"llm","prompt":"hi"}`))
	rec := httptest.NewRecorder()

	server.InferenceHandler(rec, req)

	var got inferenceResponse
	err := json.NewDecoder(rec.Body).Decode(&got)
	if err != nil {
		t.Fatalf("Got decode error %v", err)
	}

	if got.Text != "abc" {
		t.Errorf("got %q, want %q", got.Text, "abc")
	}

	if got.GeneratedTokens != 3 {
		t.Errorf("got %d, want %d", got.GeneratedTokens, 3)
	}
}

func TestInferenceHandlerStream(t *testing.T) {
	server := NewServer(&worker.Fake{
		Tokens: []string{"a", "b", "c"},
	}, DefaultTimeouts())
	req := httptest.NewRequest(http.MethodPost, "/v1/inference", strings.NewReader(`{"model":"llm","prompt":"hi", "stream":true}`))
	rec := httptest.NewRecorder()
	server.InferenceHandler(rec, req)
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("got content-type %s, want %s", rec.Header().Get("Content-Type"), "text/event-stream")
	}

	if strings.Count(body, "event: token") != 3 {
		t.Errorf("got %d token, want %d", strings.Count(body, "event: token"), 3)
	}

	if !strings.Contains(body, `data: {"text":"a"}`) {
		t.Errorf("got tokens %s, want %s", body, `data: {"text":"a"}`)
	}

	if !strings.Contains(body, "event: done") {
		t.Errorf("stream did not finish successfully, got %s, want %s", body, "event: done")
	}

	if !strings.Contains(body, `"generated_tokens":3`) {
		t.Errorf("stream finished without all tokens, got %s, want %s", body, `"generated_tokens":3`)
	}
}

func TestInferenceHandlerStreamError(t *testing.T) {
	server := NewServer(&worker.Fake{
		Tokens:    []string{"a", "b", "c"},
		FailAfter: 1,
	}, shortTimeouts())
	req := httptest.NewRequest(http.MethodPost, "/v1/inference", strings.NewReader(`{"model":"llm","prompt":"hi", "stream":true}`))
	rec := httptest.NewRecorder()
	server.InferenceHandler(rec, req)
	body := rec.Body.String()

	if strings.Count(body, "event: token") != 1 {
		t.Errorf("got %d token, want %d", strings.Count(body, "event: token"), 1)
	}

	if !strings.Contains(body, "event: error") {
		t.Errorf("stream should have error, got %s, want %s", body, "event: error")
	}

	if !strings.Contains(body, `"code":"internal_error"`) {
		t.Errorf("wrong error content, got %s, want %s", body, `"code":"internal_error"`)
	}

	if strings.Contains(body, "event: done") {
		t.Errorf("stream should not have finished, body: %q", body)
	}
}

func TestInferenceHandlerTotalTimeout(t *testing.T) {
	server := NewServer(&worker.Fake{
		Tokens: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"},
		Delay:  20 * time.Millisecond,
	}, shortTimeouts())

	req := httptest.NewRequest(http.MethodPost, "/v1/inference", strings.NewReader(`{"model":"llm","prompt":"hi"}`))
	rec := httptest.NewRecorder()
	server.InferenceHandler(rec, req)
	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("got %v, want %v", rec.Code, http.StatusGatewayTimeout)
	}

	var got errorResponse
	err := json.NewDecoder(rec.Body).Decode(&got)
	if err != nil {
		t.Fatalf("decode error %v", err)
	}

	if got.Code != "inference_timeout" {
		t.Errorf("got %q, want %q", got.Code, "inference_timeout")
	}
}

func TestInferenceHandlerStreamTotalTimeout(t *testing.T) {
	server := NewServer(&worker.Fake{
		Tokens: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"},
		Delay:  20 * time.Millisecond,
	}, shortTimeouts())

	req := httptest.NewRequest(http.MethodPost, "/v1/inference", strings.NewReader(`{"model":"llm","prompt":"hi", "stream":true}`))
	rec := httptest.NewRecorder()
	server.InferenceHandler(rec, req)
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	if !strings.Contains(body, "event: error") {
		t.Errorf("stream should have error, got %s, want %s", body, "event: error")
	}

	if !strings.Contains(body, `"code":"inference_timeout"`) {
		t.Errorf("wrong error content, got %s, want %s", body, `"code":"inference_timeout"`)
	}

	if strings.Contains(body, "event: done") {
		t.Errorf("stream should not have finished, body: %q", body)
	}
}
