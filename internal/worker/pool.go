package worker

import (
	"sync"
	"context"
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
	return nil, nil
}

func (p *Pool) inFlightCount(idx int) (Generator) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inFlight ;[idx]
}