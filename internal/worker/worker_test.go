package worker

import (
	"context"
	"testing"
	"time"
)

func TestFakeGenerate(t *testing.T) {
	fake := Fake{
		Tokens: []string{"a", "b", "c"},
		Delay:  0,
	}
	var got []Token
	ch, err := fake.Generate(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Fail to generate tokens: %v", err)
	}
	for token := range ch {
		got = append(got, token)
	}
	want := len(fake.Tokens) + 1
	if len(got) != want {
		t.Fatalf("got %d tokens, want %d", len(got), want)
	}

	for i, want := range fake.Tokens {
		if got[i].Text != want || got[i].FinishReason != "" {
			t.Errorf("token %d: got %+v, want text %q", i, got[i], want)
		}
	}

	last := got[len(got)-1]
	if last.Text != "" || last.FinishReason != "stop" {
		t.Errorf("marker: got %+v, want empty text and finish_reason stop", last)
	}
}

func TestFakeGenerateCancel(t *testing.T) {
	fake := Fake{
		Tokens: []string{"a", "b", "c"},
		Delay:  50 * time.Millisecond,
	}
	var got []string
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := fake.Generate(ctx, Request{})
	if err != nil {
		t.Fatalf("Fail to generate tokens: %v", err)
	}
	<-ch
	cancel()
	for token := range ch {
		got = append(got, token.Text)
	}
	if 1+len(got) >= len(fake.Tokens) {
		t.Errorf("Cancellation does not work, got %v, but should be < %v", 1+len(got), len(fake.Tokens))
	}
}
