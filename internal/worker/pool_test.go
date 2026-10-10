package worker

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPoolReleasesAfterNormalStream(t *testing.T) {
	p := NewPool(&Fake{Tokens: []string{"a", "b", "c"}})
	ch, err := p.Generate(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var got []Token
	for tok := range ch {
		got = append(got, tok)
	}
	if len(got) != 4 { // 3 text tokens + the final FinishReason token
		t.Fatalf("Got %d tokens, want 4", len(got))
	}
	n := p.inFlightCount(0)
	if n != 0 {
		t.Fatalf("should be 0, got %d", n)
	}
}

func TestPoolGenerateFail(t *testing.T) {
	wantErr := errors.New("down")
	p := NewPool(&Fake{Err: wantErr})
	ch, err := p.Generate(context.Background(), Request{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("want %v, got %v", wantErr, err)
	}
	if ch != nil {
		t.Fatalf("Generate returned a channel on error, want %v", wantErr)
	}
	n := p.inFlightCount(0)
	if n != 0 {
		t.Fatalf("should be 0, got %d", n)
	}
}

func TestPoolCountDuringStream(t *testing.T) {
	p := NewPool(&Fake{Tokens: []string{"a", "b", "c"}, Delay: 50 * time.Millisecond})
	ch, err := p.Generate(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	first, ok := <-ch
	if !ok {
		t.Fatalf("channel closed before first token")
	}
	if first.Text != "a" {
		t.Fatalf("first token should be %v, got %v", "a", first)
	}
	if p.inFlightCount(0) != 1 {
		t.Fatalf("should be 1, got %d", p.inFlightCount(0))
	}
	var got []Token
	for tok := range ch {
		got = append(got, tok)
	}
	if len(got) != 3 {
		t.Fatalf("should get 3 tokens, but got %d tokens", len(got))
	}
	n := p.inFlightCount(0)
	if n != 0 {
		t.Fatalf("should be 0, got %d", n)
	}
}

func TestPoolClientCancel(t *testing.T) {
	tokens := make([]string, 100)
	for i := range tokens {
		tokens[i] = "x"
	}
	p := NewPool(&Fake{Tokens: tokens, Delay: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := p.Generate(ctx, Request{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	_, ok := <-ch
	if !ok {
		t.Fatalf("channel closed before first token")
	}
	if p.inFlightCount(0) != 1 {
		t.Fatalf("should be 1, got %d", p.inFlightCount(0))
	}
	cancel()
	for range ch {
	}
	n := p.inFlightCount(0)
	if n != 0 {
		t.Fatalf("should be 0, got %d", n)
	}
}
