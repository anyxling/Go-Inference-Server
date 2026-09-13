package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestID(t *testing.T) {
	wrapped := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") == "" {
		t.Errorf("X-Request-ID should not be empty")
	}
}

func TestRequestIDWithID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "abc123")
	rec := httptest.NewRecorder()

	wrapped := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	wrapped.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") != "abc123" {
		t.Errorf("X-Request-ID incorrect")
	}
}
