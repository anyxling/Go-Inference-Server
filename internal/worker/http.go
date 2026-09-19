package worker

import (
	"context"
	"net"
	"net/http"
	"time"
)

var _ Generator = (*HTTPClient)(nil)

type HTTPClient struct {
	url    string
	client *http.Client
}

func NewHTTPClient(baseURL string, connectTimeout time.Duration) *HTTPClient {
	dialer := net.Dialer{Timeout: connectTimeout}
	transport := &http.Transport{DialContext: dialer.DialContext}
	client := &http.Client{Transport: transport}
	return &HTTPClient{url: baseURL, client: client}
}

func (c *HTTPClient) Generate(ctx context.Context, req Request) (<-chan Token, error) {
	return nil, nil
}
