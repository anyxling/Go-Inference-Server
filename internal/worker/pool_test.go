package worker

import (
	"testing"
	"context"
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