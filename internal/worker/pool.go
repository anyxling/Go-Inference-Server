package worker

import (
	"context"
	"sync"
)

type Pool struct {
	mu       sync.Mutex // mutex guards inFlight
	workers  []Generator
	inFlight []int
	next     int
}

var _ Generator = (*Pool)(nil)

func NewPool(workers ...Generator) *Pool {
	if len(workers) == 0 {
		panic("no workers in the pool")
	}
	inFlight := make([]int, len(workers))
	return &Pool{workers: workers, inFlight: inFlight}
}

func (p *Pool) pick() int {
	p.mu.Lock()
	best := -1
	for k := 0; k < len(p.inFlight); k++ {
		i := (p.next + k) % len(p.inFlight)
		if best == -1 || p.inFlight[i] < p.inFlight[best] {
			best = i
		}
	}
	p.inFlight[best]++
	p.next = (best + 1) % len(p.inFlight)
	p.mu.Unlock()
	return best
}

func (p *Pool) release(i int) {
	p.mu.Lock()
	p.inFlight[i]--
	p.mu.Unlock()
}

func (p *Pool) Generate(ctx context.Context, req Request) (<-chan Token, error) {
	i := p.pick()
	upstream, err := p.workers[i].Generate(ctx, req)
	if err != nil {
		p.release(i)
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
		defer p.release(i)
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
