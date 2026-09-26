package worker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// Must match worker/server.py byte for byte, or the two workers aren't comparable.
const systemPrompt = "You are Qwen, created by Alibaba Cloud. You are a helpful assistant."

var _ Generator = (*VLLMClient)(nil)

type VLLMClient struct {
	url    string
	model  string
	client *http.Client
}

type vllmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type vllmRequest struct {
	Model         string        `json:"model"`
	Messages      []vllmMessage `json:"messages"`
	MaxTokens     int           `json:"max_tokens"`
	Temperature   float64       `json:"temperature"`
	Stream        bool          `json:"stream"`
	StreamOptions struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
}

type vllmChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func NewVLLMClient(baseURL string, model string, connectTimeout time.Duration) *VLLMClient {
	dialer := net.Dialer{Timeout: connectTimeout}
	transport := &http.Transport{DialContext: dialer.DialContext}
	client := &http.Client{Transport: transport}
	return &VLLMClient{url: baseURL, model: model, client: client}
}

func (c *VLLMClient) Generate(ctx context.Context, req Request) (<-chan Token, error) {
	var finishReason string
	var generated int

	body := vllmRequest{
		Model: c.model,
		Messages: []vllmMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: req.Prompt},
		},
		MaxTokens:   req.MaxOutputTokens,
		Temperature: req.Temperature,
		Stream:      true,
	}
	body.StreamOptions.IncludeUsage = true
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/v1/chat/completions", bytes.NewReader(b))
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
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue // blank separator lines
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				send(Token{FinishReason: finishReason, GeneratedTokens: generated})
				return
			}
			var chunk vllmChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				send(Token{Err: err})
				return
			}
			if chunk.Usage != nil {
				generated = chunk.Usage.CompletionTokens
			}
			if len(chunk.Choices) == 0 {
				continue // the usage-only chunk
			}
			choice := chunk.Choices[0]
			if choice.FinishReason != nil {
				finishReason = *choice.FinishReason
			}
			if choice.Delta.Content != "" {
				if !send(Token{Text: choice.Delta.Content}) {
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
