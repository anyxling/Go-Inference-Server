package worker

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPClientGenerate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `{"text":"I want"}`)
		flusher.Flush()
		fmt.Fprintln(w, `{"text":"a new job"}`)
		flusher.Flush()
		fmt.Fprintln(w, `{"text":"immediately"}`)
		flusher.Flush()
		fmt.Fprintln(w, `{"done":true, "finish_reason":"stop"}`)
		flusher.Flush()

	}))
	defer ts.Close()

	c := NewHTTPClient(ts.URL, time.Second)
	ch, err := c.Generate(context.Background(), Request{})
	if err != nil {
		t.Fatalf("token generation fail with %v", err)
	}

	var got []Token
	for token := range ch {
		got = append(got, token)
	}

	wantTexts := []string{"I want", "a new job", "immediately"}

	want := len(wantTexts) + 1
	if len(got) != want {
		t.Fatalf("got %d tokens, want %d", len(got), want)
	}

	for i, want := range wantTexts {
		if got[i].Text != want || got[i].FinishReason != "" {
			t.Errorf("token %d: got %+v, want text %q", i, got[i], want)
		}
	}

	last := got[len(got)-1]
	if last.Text != "" || last.FinishReason != "stop" {
		t.Errorf("marker: got %+v, want empty text and finish_reason stop", last)
	}
}

func TestHTTPClientGenerateBadStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	c := NewHTTPClient(ts.URL, time.Second)
	ch, err := c.Generate(context.Background(), Request{})
	if err == nil {
		t.Errorf("zero error, should get error")
	}
	if ch != nil {
		t.Errorf("channel should be empty, got %v", ch)
	}
}

func TestHTTPClientGenerateErrorLine(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `{"text":"a"}`)
		flusher.Flush()
		fmt.Fprintln(w, `{"error":"gpu on fire"}`)
		flusher.Flush()
	}))
	defer ts.Close()
	c := NewHTTPClient(ts.URL, time.Second)
	ch, err := c.Generate(context.Background(), Request{})
	if err != nil {
		t.Fatalf("token generation fail with %v", err)
	}
	var got []Token
	for token := range ch {
		got = append(got, token)
	}
	if len(got) != 2 {
		t.Fatalf("Should get 2 tokens, but get %v", len(got))
	}
	if got[0].Text != "a" || got[0].Err != nil {
		t.Fatalf("the first token should be non-empty with zero error, but got text %v and error %v", got[0].Text, got[0].Err)
	}

	if !strings.Contains(got[1].Err.Error(), "gpu on fire") {
		t.Fatalf("the second token should be error with text %v, but get %v", "gpu on fire", got[1].Err.Error())
	}
}

func TestHTTPClientGenerateCancel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		for i := 0; i < 5; i++ {
			select {
			case <-r.Context().Done():
				return // client went away; stop like a real worker should
			case <-time.After(100 * time.Millisecond):
			}
			fmt.Fprintln(w, `{"text":"a"}`)
			flusher.Flush()
		}
		fmt.Fprintln(w, `{"done":true}`)
		flusher.Flush()
	}))
	defer ts.Close()

	c := NewHTTPClient(ts.URL, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := c.Generate(ctx, Request{})
	if err != nil {
		t.Fatalf("token generation fail with %v", err)
	}

	first := <-ch // block until the first token arrives, ~100ms
	cancel()

	var rest []Token
	for token := range ch { // drains until the goroutine closes ch
		rest = append(rest, token)
	}

	if first.Text == "" || first.Err != nil {
		t.Fatalf("the first token should be non-empty with zero error, but got text %v and error %v", first.Text, first.Err)
	}

	if 1+len(rest) >= 5 {
		t.Fatalf("cancellation did not stop the stream, should have less than 5 tokens but got %v", len(rest))
	}
}
