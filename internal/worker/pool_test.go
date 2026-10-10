package worker

import (
	"context"
	"errors"
	"sync"
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

func TestPoolEvenSplit(t *testing.T) {
	long := make([]string, 100)
	for i := range long {
		long[i] = "x"
	}
	p := NewPool(
		&Fake{Tokens: long, Delay: 50 * time.Millisecond},
		&Fake{Tokens: long, Delay: 50 * time.Millisecond},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const n = 10
	chans := make([]<-chan Token, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ch, err := p.Generate(ctx, Request{})
			if err != nil {
				t.Errorf("Generate: %v", err)
				return
			}
			chans[i] = ch
		}(i)
	}
	wg.Wait()
	if p.inFlightCount(0) != 5 {
		t.Fatalf("request counts for 1st worker should be 5, got %v", p.inFlight[0])
	}
	if p.inFlightCount(1) != 5 {
		t.Fatalf("request counts for 2nd worker should be 5, got %v", p.inFlight[1])
	}
	cancel()
}

func TestPoolFreedWorkerGetNextReq(t *testing.T) {
	long := make([]string, 100)
	for i := range long {
		long[i] = "x"
	}
	p := NewPool(
		&Fake{Tokens: long, Delay: 50 * time.Millisecond},
		&Fake{Tokens: long, Delay: 50 * time.Millisecond},
	)
	// First request: both idle, rotation starts at 0, so it goes to worker 0.
	ctx0, cancel0 := context.WithCancel(context.Background())
	defer cancel0()
	_, err := p.Generate(ctx0, Request{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Second request: worker 1 is less busy, so it goes there.
	ctx1, cancel1 := context.WithCancel(context.Background())
	ch1, err := p.Generate(ctx1, Request{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if p.inFlightCount(0) != 1 || p.inFlightCount(1) != 1 {
		t.Fatalf("after two requests: counts = %d, %d; want 1, 1", p.inFlightCount(0), p.inFlightCount(1))
	}
	// Free worker 1.
	cancel1()
	for range ch1 {
	}

	// A new request
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	_, err = p.Generate(ctx2, Request{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if p.inFlightCount(1) != 1 {
		t.Fatalf("worker 1 count = %d, want 1: the freed worker should get the next request", p.inFlightCount(1))
	}
	if p.inFlightCount(0) != 1 {
		t.Fatalf("worker 0 count = %d, want 1: the freed worker should get the next request", p.inFlightCount(0))
	}
}

// When every worker is idle, all counts tie at 0. Least-in-flight alone would
// send every request to worker 0; the round-robin tie-break should rotate them.
func TestPoolTiesRotate(t *testing.T) {
	// Each worker answers with its own name, so the first token says who served the request.
	p := NewPool(
		&Fake{Tokens: []string{"w0"}},
		&Fake{Tokens: []string{"w1"}},
	)

	want := []string{"w0", "w1", "w0", "w1", "w0", "w1"}
	for n, w := range want {
		ch, err := p.Generate(context.Background(), Request{})
		if err != nil {
			t.Fatalf("request %d: Generate: %v", n, err)
		}
		first := <-ch
		// Drain to the end so the worker is released before the next request:
		// that is what makes every request see a tie.
		for range ch {
		}
		if first.Text != w {
			t.Fatalf("request %d served by %q, want %q", n, first.Text, w)
		}
		if a, b := p.inFlightCount(0), p.inFlightCount(1); a != 0 || b != 0 {
			t.Fatalf("after request %d: counts = %d, %d; want 0, 0", n, a, b)
		}
	}
}
