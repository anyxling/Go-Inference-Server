package worker

import (
	"context"
	"time"
)

type Generator interface {
	Generate(ctx context.Context, req Request) (<-chan Token, error)
}

type Fake struct {
	Tokens []string
	Delay  time.Duration
}

var _ Generator = (*Fake)(nil)

func (f *Fake) Generate(ctx context.Context, req Request) (<-chan Token, error) {
	ch := make(chan Token)
	go func() {
		defer close(ch)
		for _, token := range f.Tokens {
			time.Sleep(f.Delay)
			select {
			case ch <- Token{Text: token}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}
