package worker

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestFakeGenerate(t *testing.T) {
	fake := Fake{
		Tokens: []string{"a", "b", "c"},
		Delay:  0,
	}
	var got []string
	ch, err := fake.Generate(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Fail to generate tokens: %v", err)
	}
	for token := range ch {
		got = append(got, token.Text)
	}
	if !slices.Equal(got, fake.Tokens) {
		t.Errorf("Got %v but it should be %v", got, fake.Tokens)
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
