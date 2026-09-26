package main

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
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
	prompt := flag.String("prompt", "Write a long essay about the history of computers.", "prompt; should reliably fill max-tokens so every request does equal work")
	maxTokens := flag.Int("max-tokens", 128, "the max number of output tokens for LLM")
	warmup := flag.Int("warmup", 1, "warm-up requests sent before timing starts; their results are discarded")
	csvPath := flag.String("csv", "", "write one line per request to this file (start offset, ttft, latency, tokens, finish reason)")

	flag.Parse()

	body := fmt.Sprintf(`{"model":"qwen","prompt":%q,"max_output_tokens":%d,"stream":true}`, *prompt, *maxTokens)
	client := &http.Client{Timeout: 0, Transport: &http.Transport{MaxIdleConnsPerHost: *concurrency}}

	// Warm-up: the first request after a restart pays one-off costs (CUDA
	// context, kernel compilation, allocator growth) that are not part of
	// steady-state throughput.
	for i := 0; i < *warmup; i++ {
		r := doRequest(client, *url, body)
		if r.err != nil || r.status != http.StatusOK {
			log.Fatalf("warm-up request %d failed: status=%d err=%v", i+1, r.status, r.err)
		}
	}

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
	var totalTokens, rejected, failed, fullLength int
	for _, r := range all {
		switch {
		case r.err != nil:
			failed++
		case r.status == http.StatusServiceUnavailable:
			rejected++
		case r.status == http.StatusOK:
			ttfts = append(ttfts, r.first.Sub(r.start))
			latencies = append(latencies, r.end.Sub(r.start))
			if r.generated >= 2 {
				rates = append(rates, float64(r.generated-1)/r.end.Sub(r.first).Seconds())
			}
			totalTokens += r.generated
			if r.finishReason == "length" {
				fullLength++
			}
		}
	}

	// summarize
	wall := finish.Sub(begin) // recorded around the pool
	aggregate := float64(totalTokens) / wall.Seconds()

	fmt.Printf("concurrency=%d requests=%d ok=%d rejected=%d failed=%d length=%d/%d\n", *concurrency, *requests, len(ttfts), rejected, failed, fullLength, len(ttfts))
	fmt.Printf("ttft      p50=%v p95=%v\n", percentile(ttfts, 0.5), percentile(ttfts, 0.95))
	fmt.Printf("latency   p50=%v p95=%v\n", percentile(latencies, 0.5), percentile(latencies, 0.95))
	fmt.Printf("per-stream tok/s p50=%.1f\n", percentile(rates, 0.5))
	fmt.Printf("aggregate  tok/s %.1f\n", aggregate)
	if fullLength != len(ttfts) {
		fmt.Printf("WARNING: %d request(s) stopped before max-tokens; work per request was not equal\n", len(ttfts)-fullLength)
	}

	if *csvPath != "" {
		if err := writeCSV(*csvPath, all, begin); err != nil {
			log.Fatalf("write csv: %v", err)
		}
	}
}

// writeCSV records one line per request so latency can be examined against
// start time: a drift over the run points to throttling, a bimodal spread with
// no trend points to unfair scheduling between concurrent generations.
func writeCSV(path string, all []result, begin time.Time) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	slices.SortFunc(all, func(a, b result) int { return a.start.Compare(b.start) })

	w := bufio.NewWriter(f)
	fmt.Fprintln(w, "start_offset_ms,ttft_ms,latency_ms,generated_tokens,finish_reason,status,error")
	for _, r := range all {
		var ttft, latency float64
		if !r.first.IsZero() {
			ttft = float64(r.first.Sub(r.start).Microseconds()) / 1000
		}
		if !r.end.IsZero() {
			latency = float64(r.end.Sub(r.start).Microseconds()) / 1000
		}
		errText := ""
		if r.err != nil {
			errText = strings.ReplaceAll(r.err.Error(), ",", ";")
		}
		fmt.Fprintf(w, "%.1f,%.1f,%.1f,%d,%s,%d,%s\n",
			float64(r.start.Sub(begin).Microseconds())/1000, ttft, latency, r.generated, r.finishReason, r.status, errText)
	}
	return w.Flush()
}
