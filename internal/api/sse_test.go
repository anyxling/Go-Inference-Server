package api

import (
	"bytes"
	"testing"
)

func TestWriteSSE(t *testing.T) {
	var buf bytes.Buffer
	err := writeSSE(&buf, "token", map[string]string{"text": "hi"})
	if err != nil {
		t.Fatalf("Got error %v", err)
	}
	if buf.String() != "event: token\ndata: {\"text\":\"hi\"}\n\n" {
		t.Errorf("got %q, want %q", buf.String(), "event: token\ndata: {\"text\":\"hi\"}\n\n")
	}
}
