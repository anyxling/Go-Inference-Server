package main

import (
	"bufio"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

type result struct {
	start, first, end time.Time
	finishReason      string
	generated         int
	tokens            int
	status            int
	err               error
}

type doneData struct {
	FinishReason    string `json:"finish_reason"`
	GeneratedTokens int    `json:"generated_tokens"`
}

func doRequest(client *http.Client, url, body string) result {
	res := result{}
	res.start = time.Now()

	resp, err := client.Post(url+"/v1/inference", "application/json", strings.NewReader(body))

	if err != nil {
		res.err = err
		return res
	}

	defer resp.Body.Close()

	res.status = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		return res
	}

	scanner := bufio.NewScanner(resp.Body)
	var event string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data := strings.TrimPrefix(line, "data: ")
			switch event {
			case "token":
				if res.tokens == 0 {
					res.first = time.Now()
				}
				res.tokens++
			case "done":
				var d doneData
				if err := json.Unmarshal([]byte(data), &d); err != nil {
					res.err = fmt.Errorf("bad done event: %w", err)
					return res
				}
				res.finishReason = d.FinishReason
				res.generated = d.GeneratedTokens
				res.end = time.Now()
				return res
			case "error":
				res.err = errors.New("stream error: " + data)
				return res
			}
		}
	}
	if scanner.Err() != nil {
		res.err = scanner.Err()
	}
	if scanner.Err() == nil && res.end.IsZero() {
		res.err = errors.New("stream ended without done")
	}
	return res
}

func percentile[T cmp.Ordered](xs []T, p float64) T {
	if len(xs) == 0 {
		var zero T
		return zero
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	i := int(p * float64(len(s)-1))
	return s[i]
}

func main() {
	url := flag.String("url", "http://localhost:8080", "base url")
	concurrency := flag.Int("concurrency", 1, "number of concurrent requests")
	requests := flag.Int("requests", 10, "total number of requests")
	prompt := flag.String("prompt", "You are Qwen, created by Alibaba Cloud. You are a helpful assistant.", "prompt")
	maxTokens := flag.Int("max-tokens", 128, "the max number of output tokens for LLM")

	flag.Parse()

	body := fmt.Sprintf(`{"model":"qwen","prompt":%q,"max_output_tokens":%d,"stream":true}`, *prompt, *maxTokens)
	client := &http.Client{Timeout: 0, Transport: &http.Transport{MaxIdleConnsPerHost: *concurrency}}

	var wg sync.WaitGroup
	jobs := make(chan struct{})
	results := make(chan result, *requests)
	begin := time.Now()

	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				results <- doRequest(client, *url, body)
			}
		}()
	}

	for i := 0; i < *requests; i++ {
		jobs <- struct{}{}
	}
	close(jobs)

	wg.Wait()
	finish := time.Now()
	close(results)

	// collect
	var all []result
	for r := range results {
		all = append(all, r)
	}

	// classify
	var ttfts, latencies []time.Duration
	var rates []float64
	var totalTokens, rejected, failed int
	for _, r := range all {
		switch {
		case r.err != nil:
			failed++
		case r.status == http.StatusServiceUnavailable:
			rejected++
		case r.status == http.StatusOK:
			ttfts = append(ttfts, r.first.Sub(r.start))
			latencies = append(latencies, r.end.Sub(r.start))
			if r.tokens >= 2 {
				rates = append(rates, float64(r.tokens-1)/r.end.Sub(r.first).Seconds())
			}
			totalTokens += r.tokens
		}
	}

	// summarize
	wall := finish.Sub(begin) // recorded around the pool
	aggregate := float64(totalTokens) / wall.Seconds()

	fmt.Printf("concurrency=%d requests=%d ok=%d rejected=%d failed=%d\n", *concurrency, *requests, len(ttfts), rejected, failed)
	fmt.Printf("ttft      p50=%v p95=%v\n", percentile(ttfts, 0.5), percentile(ttfts, 0.95))
	fmt.Printf("latency   p50=%v p95=%v\n", percentile(latencies, 0.5), percentile(latencies, 0.95))
	fmt.Printf("per-stream tok/s p50=%.1f\n", percentile(rates, 0.5))
	fmt.Printf("aggregate  tok/s %.1f\n", aggregate)

}
