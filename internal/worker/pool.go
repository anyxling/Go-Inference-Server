package worker

import (
	"context"
	"sync"
)

type Pool struct {
	mu       sync.Mutex // mutex guards inFlight
	workers  []Generator
	inFlight []int
}

var _ Generator = (*Pool)(nil)

func NewPool(workers ...Generator) *Pool {
	if len(workers) == 0 {
		panic("no workers in the pool")
	}
	inFlight := make([]int, len(workers))
	return &Pool{workers: workers, inFlight: inFlight}
}

func (p *Pool) acquire(i int) {
	p.mu.Lock()
	p.inFlight[i]++
	p.mu.Unlock()
}

func (p *Pool) release(i int) {
	p.mu.Lock()
	p.inFlight[i]--
	p.mu.Unlock()
}

func (p *Pool) Generate(ctx context.Context, req Request) (<-chan Token, error) {
	p.acquire(0)
	upstream, err := p.workers[0].Generate(ctx, req)
	if err != nil {
		p.release(0)
		return nil, err
	}
	out := make(chan Token)
	send := func(t Token) bool {
		select {
		case out <- t:
			return true
		case <-ctx.Done():
			return false
		}
	}
	go func() {
		defer close(out)
		defer p.release(0)
		for token := range upstream {
			send(token)
		}
	}()
	return out, nil
}

func (p *Pool) inFlightCount(i int) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inFlight[i]
}
