package worker

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFakeGenErr(t *testing.T) {
	fake := Fake{
		Tokens: []string{"a", "b", "c"},
		Delay:  50 * time.Millisecond,
		Err:    ErrFakeFailure,
	}
	ch, err := fake.Generate(context.Background(), Request{})
	if !errors.Is(err, ErrFakeFailure) {
		t.Errorf("got err=%v, want %v", err, ErrFakeFailure)
	}
	if ch != nil {
		t.Errorf("got ch=%v, want nil channel", ch)
	}
}

func TestFakeFailAfter(t *testing.T) {
	fake := Fake{
		Tokens:    []string{"a", "b", "c"},
		Delay:     0,
		FailAfter: 1,
	}
	var got []Token
	ch, err := fake.Generate(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Should not get error but got %v", err)
	}
	for token := range ch {
		got = append(got, token)
	}
	if len(got) != 2 {
		t.Fatalf("Should get 2 tokens, but get %v", len(got))
	}
	if got[0].Text != "a" || got[0].Err != nil {
		t.Fatalf("The first token should be a and the error should be null, but got %v", got[0].Text)
	}
	if !errors.Is(got[1].Err, ErrFakeFailure) {
		t.Fatalf("The second token should have ErrFakeFailure but got %v", got[1].Err)
	}
}
