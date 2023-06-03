package scrypted_arlo_go

import (
	"fmt"
	"io"
	"net/http"

	"github.com/CUCyber/ja3transport"
)

type HTTPClient struct {
	headers   HeadersMap
	ja3client *ja3transport.JA3Client
}

func NewHTTPClient(headers HeadersMap) (*HTTPClient, error) {
	ja3c, err := ja3transport.New(ja3transport.SafariAuto)
	if err != nil {
		return nil, fmt.Errorf("could not create ja3client: %w", err)
	}
	return &HTTPClient{
		headers:   headers,
		ja3client: ja3c,
	}, nil
}

func (h *HTTPClient) Get(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	for k, v := range h.headers {
		req.Header.Set(k, v)
	}

	resp, err := h.ja3client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code from request: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("could not read response body: %w", err)
	}

	return string(body), nil
}
