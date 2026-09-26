package worker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

var _ Generator = (*HTTPClient)(nil)

type HTTPClient struct {
	url    string
	client *http.Client
}

type workerLine struct {
	Text            string `json:"text"`
	Done            bool   `json:"done"`
	GeneratedTokens int    `json: generated_tokens`
	FinishReason    string `json:"finish_reason"`
	Error           string `json:"error"`
}

func NewHTTPClient(baseURL string, connectTimeout time.Duration) *HTTPClient {
	dialer := net.Dialer{Timeout: connectTimeout}
	transport := &http.Transport{DialContext: dialer.DialContext}
	client := &http.Client{Transport: transport}
	return &HTTPClient{url: baseURL, client: client}
}

func (c *HTTPClient) Generate(ctx context.Context, req Request) (<-chan Token, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/generate", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request send fail %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		httpResp.Body.Close()
		return nil, fmt.Errorf("response status wrong %d", httpResp.StatusCode)
	}

	ch := make(chan Token)
	send := func(t Token) bool {
		select {
		case ch <- t:
			return true
		case <-ctx.Done():
			return false
		}
	}
	go func() {
		defer close(ch)
		defer httpResp.Body.Close()

		scanner := bufio.NewScanner(httpResp.Body)
		for scanner.Scan() {
			var line workerLine
			err := json.Unmarshal(scanner.Bytes(), &line)
			if err != nil {
				send(Token{Err: err})
				return
			}
			switch {
			case line.Error != "":
				send(Token{Err: errors.New(line.Error)})
				return
			case line.Done:
				send(Token{FinishReason: line.FinishReason})
				return
			default:
				if !send(Token{Text: line.Text}) {
					return
				}
			}
		}
		err := scanner.Err()
		if err != nil && ctx.Err() == nil {
			send(Token{Err: err})
			return
		}
	}()
	return ch, nil
}
