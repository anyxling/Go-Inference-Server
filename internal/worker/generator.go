package worker

import (
	"context"
	"errors"
	"time"
)

type Generator interface {
	Generate(ctx context.Context, req Request) (<-chan Token, error)
}

type Fake struct {
	Tokens    []string
	Delay     time.Duration
	Err       error
	FailAfter int
}

var _ Generator = (*Fake)(nil)

var ErrFakeFailure = errors.New("fake worker failure")

func (f *Fake) Generate(ctx context.Context, req Request) (<-chan Token, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	ch := make(chan Token)
	go func() {
		defer close(ch)
		for i, token := range f.Tokens {
			if f.FailAfter != 0 && i >= f.FailAfter {
				select {
				case <-time.After(f.Delay):
				case <-ctx.Done():
					return
				}
				select {
				case ch <- Token{Err: ErrFakeFailure}:
				case <-ctx.Done():
					return
				}
				return
			}
			select {
			case <-time.After(f.Delay):
			case <-ctx.Done():
				return
			}
			select {
			case ch <- Token{Text: token}:
			case <-ctx.Done():
				return
			}
		}
		select {
		case ch <- Token{FinishReason: "stop"}:
		case <-ctx.Done():
			return
		}
	}()
	return ch, nil
}
