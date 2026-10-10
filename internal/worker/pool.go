package worker

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
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
//
// SHA-256 rather than a fast hash like FNV: FNV mixes its input too weakly for
// comparing scores (it sent ~80% of short keys to one worker), and hash/maphash
// is randomly seeded per process, so keys would move to new workers on every
// restart. The ~0.6 µs per score is negligible next to an inference request.
// The "%d:" separator keeps worker 1 + "2x" distinct from worker 12 + "x".
func score(key string, i int) uint64 {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", i, key)))
	return binary.BigEndian.Uint64(sum[:8])
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
