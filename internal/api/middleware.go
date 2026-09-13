package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

func newRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		val := r.Header.Get("X-Request-ID")
		if val == "" {
			val = newRequestID()
		}
		w.Header().Set("X-Request-ID", val)
		next.ServeHTTP(w, r)
	})
}
