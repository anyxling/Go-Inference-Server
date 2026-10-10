package worker

import (
	"context"
	"hash/fnv"
	"sync"
)

type Pool struct {
	mu       sync.Mutex // mutex guards inFlight
	workers  []Generator
	inFlight []int
	next     int
	Affinity bool // route by prompt prefix instead of least-in-flight
}

var _ Generator = (*Pool)(nil)

const affinityPrefixLen = 512

func affinityKey(prompt string) string {
	if len(prompt) > affinityPrefixLen {
		return prompt[:affinityPrefixLen]
	}
	return prompt
}

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
	var i int
	if p.Affinity {
		i = p.pickFor(affinityKey(req.Prompt))
	} else {
		i = p.pick()
	}
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

// score is the rendezvous-hashing weight of worker i for key. For each key,
// the worker with the highest score is its preferred worker.
func score(key string, i int) uint64 {
	h := fnv.New64a()
	h.Write([]byte{byte(i)}) // worker first: the key's bytes then mix the difference through the whole hash
	h.Write([]byte(key))
	return h.Sum64()
}

func (p *Pool) pickFor(key string) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.inFlight)
	total := 0
	for i := 0; i < n; i++ {
		total += p.inFlight[i]
	}
	limit := (5*(total+1) + 4*n - 1) / (4 * n)
	best, bestScore := -1, uint64(0)
	for i := 0; i < n; i++ {
		if p.inFlight[i] < limit {
			s := score(key, i)
			if best == -1 || s > bestScore {
				best, bestScore = i, s
			}
		}
	}
	p.inFlight[best]++
	return best
}
